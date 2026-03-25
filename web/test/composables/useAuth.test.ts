/**
 * Test suite for the `useAuth` composable.
 *
 * FILE: web/test/composables/useAuth.test.ts
 *
 * @vitest-environment nuxt
 *
 * WHY `@vitest-environment nuxt`?
 * ────────────────────────────────
 * `lib/api.ts` calls `useRuntimeConfig()` (a Nuxt auto-import) to build the
 * API base URL.  In the default happy-dom environment, Nuxt's module resolution
 * means that after `vi.resetModules()` the freshly-loaded api.ts imports the
 * real `useRuntimeConfig` from `nuxt/dist/app/nuxt.js`, which throws
 * "[nuxt] instance unavailable" because there is no active Nuxt app context.
 *
 * The `nuxt` Vitest environment spun up by `@nuxt/test-utils` provides a real
 * (but lightweight) Nuxt app instance that satisfies those calls, so all our
 * composable tests run cleanly.
 *
 * WHAT IS BEING TESTED
 * ────────────────────
 * `useAuth` (web/composables/useAuth.ts) manages the entire authentication
 * lifecycle:
 *   - Logging in with email + password
 *   - Handling the MFA challenge flow (TOTP second factor)
 *   - Fetching and storing the authenticated user profile
 *   - Logging out and clearing all local state
 *   - Checking whether the current user has a required role
 *
 * WHY MODULE ISOLATION IS REQUIRED
 * ─────────────────────────────────
 * The `user`, `isAuthenticated`, and `isLoading` refs are declared at the
 * TOP LEVEL of useAuth.ts — OUTSIDE the `useAuth()` function.  This means they
 * are module-level singletons: once a module is imported, its top-level code
 * runs exactly once and the same `ref` instances are shared for the lifetime of
 * the test runner.
 *
 * Consequence: if test A calls login() and sets user.value = { id: '1', … },
 * then test B imports the same module and checks user.value — it would still
 * be `{ id: '1' }` from the previous test.  Tests would depend on execution
 * order, making them fragile and hard to debug.
 *
 * Solution — `vi.resetModules()` + dynamic import:
 *   1. `vi.resetModules()` tells Vitest to discard all cached module instances.
 *   2. We then `await import(...)` to get a FRESH copy of useAuth.ts with
 *      brand-new `ref()` calls (user = null, isLoading = false).
 *   3. We also re-import lib/api.ts because it, too, is cached by the module
 *      system. After resetModules, the new useAuth.ts will load a new api.ts
 *      instance, so our `vi.stubGlobal('fetch', ...)` replacements work cleanly.
 *
 * Each test therefore operates against a completely fresh module graph.
 *
 * MOCK STRATEGY
 * ─────────────
 * `lib/api.ts` uses the native browser `fetch()` function.
 * `test/setup.ts` already mocks `navigateTo`, `useRuntimeConfig`, and the
 * Vue reactivity globals.  Here we additionally stub `fetch` with our
 * `createMockFetch` helper so no real network calls are made.
 *
 * Route matching in `createMockFetch` uses URL *suffix* matching, so we can
 * write '/auth/login' instead of 'http://localhost:8080/api/v1/auth/login'.
 */

import { createMockFetch } from '../helpers/mock-api';

// ── Shared test data ──────────────────────────────────────────────────────────

/**
 * A fully-populated user profile as returned by GET /users/me.
 *
 * Note: the API wraps the user in a `data` envelope: `{ data: <User> }`.
 * `useAuth.fetchProfile()` reads `response.data` which is why we need the
 * wrapper below.
 */
const MOCK_USER = {
  id: '1',
  schoolId: 's1',
  role: 'admin' as const,
  email: 'a@test.ro',
  firstName: 'A',
  lastName: 'B',
  totpEnabled: false,
};

/**
 * The /users/me response envelope that fetchProfile() expects.
 * fetchProfile calls: api<{ data: User }>('/users/me')
 * and reads:          user.value = data.data
 * So the mock must return: { data: <User> }
 */
const USERS_ME_RESPONSE = { data: MOCK_USER };

/**
 * A successful login response — server returns both tokens directly.
 * No MFA challenge is present (mfa_required is absent / false).
 */
const LOGIN_SUCCESS_RESPONSE = {
  access_token: 'at',
  refresh_token: 'rt',
};

/**
 * A login response that signals MFA is required.
 * The server does NOT return real tokens yet — only the mfa_token used in the
 * next step.
 */
const LOGIN_MFA_RESPONSE = {
  mfa_required: true,
  mfa_token: 'mfa-tok',
};

/**
 * Response from POST /auth/2fa/login — the second factor was accepted.
 * Now the server returns the real access + refresh tokens.
 */
const MFA_VERIFY_RESPONSE = {
  access_token: 'at-mfa',
  refresh_token: 'rt-mfa',
};

// ── Module re-import plumbing ─────────────────────────────────────────────────

/**
 * We declare `useAuth` with the type of the named export from the module.
 *
 * Using `typeof import(...)['useAuth']` means TypeScript knows the exact
 * signature (return type, parameter types) without us having to duplicate it.
 * The actual value is assigned in beforeEach after each module reset.
 */
let useAuth: (typeof import('~/composables/useAuth'))['useAuth'];

/**
 * Before each individual test:
 *  1. Reset the module registry so the next dynamic import gets fresh state.
 *  2. Dynamically import the composable so `user`, `isAuthenticated`, and
 *     `isLoading` start at their initial values (null / false).
 *
 * NOTE: `vi.clearAllMocks()` and `localStorage.clear()` are already called
 * globally in test/setup.ts — we do NOT need to repeat them here.  We only
 * need to handle the module-level state that setup.ts cannot know about.
 */
beforeEach(async () => {
  // Discard every cached module so the next import is brand-new.
  vi.resetModules();

  // Dynamically import the now-fresh module and grab its named export.
  const mod = await import('~/composables/useAuth');
  useAuth = mod.useAuth;
});

// ── Test suite ────────────────────────────────────────────────────────────────

describe('useAuth', () => {
  // ── Test 1 ─────────────────────────────────────────────────────────────────

  it('login() stores tokens and fetches profile', async () => {
    /**
     * SCENARIO
     * ────────
     * The user submits correct credentials.  The server responds with both
     * tokens immediately (no MFA challenge).  useAuth must:
     *   1. Store the tokens in localStorage via setTokens().
     *   2. Fetch the profile from /users/me and store it in user.value.
     *   3. Set isAuthenticated to true.
     *
     * MOCK SETUP
     * ──────────
     * Two routes are registered:
     *   POST /auth/login  → returns access_token + refresh_token
     *   GET  /users/me    → returns { data: <User> }
     *
     * We also need a catch-all for /auth/logout because the api() call in
     * logout() must not blow up if it's accidentally triggered (it won't be
     * here, but good practice to understand). We do NOT add it — the test
     * doesn't call logout so no extra route is needed.
     */
    vi.stubGlobal(
      'fetch',
      createMockFetch({
        // POST /auth/login — happy path, returns tokens directly
        '/auth/login': {
          status: 200,
          body: LOGIN_SUCCESS_RESPONSE,
        },
        // GET /users/me — returns profile wrapped in { data: ... }
        '/users/me': {
          status: 200,
          body: USERS_ME_RESPONSE,
        },
      }),
    );

    // Invoke the composable and call login().
    const auth = useAuth();
    const result = await auth.login('a@test.ro', 'password123');

    // ── Assertions ──────────────────────────────────────────────────────────

    // login() should return { mfaRequired: false } on the happy path.
    expect(result).toEqual({ mfaRequired: false });

    // The user should now be considered authenticated.
    expect(auth.isAuthenticated.value).toBe(true);

    // The user profile should be populated with the mock data.
    expect(auth.user.value).toEqual(MOCK_USER);

    // With cookie-based auth, tokens are set via httpOnly cookies by the API.
    // localStorage is no longer used for token storage.
  });

  // ── Test 2 ─────────────────────────────────────────────────────────────────

  it('login() with MFA returns mfa_required status', async () => {
    /**
     * SCENARIO
     * ────────
     * The user's account requires a second factor (TOTP).  The server responds
     * with { mfa_required: true, mfa_token: 'mfa-tok' } instead of real tokens.
     *
     * useAuth.login() must:
     *   1. Detect the MFA flag and return { mfaRequired: true, mfaToken }.
     *   2. NOT call setTokens() — no tokens should be in localStorage.
     *   3. NOT set user.value — isAuthenticated stays false.
     *
     * There is NO /users/me route registered here intentionally: if the code
     * accidentally calls fetchProfile(), createMockFetch will throw an error
     * about an unregistered route and the test will fail — giving us a clear
     * signal that something is wrong.
     */
    vi.stubGlobal(
      'fetch',
      createMockFetch({
        '/auth/login': {
          status: 200,
          body: LOGIN_MFA_RESPONSE,
        },
      }),
    );

    const auth = useAuth();
    const result = await auth.login('a@test.ro', 'password123');

    // ── Assertions ──────────────────────────────────────────────────────────

    // The composable should have detected mfa_required and returned the token.
    expect(result).toEqual({ mfaRequired: true, mfaToken: 'mfa-tok' });

    // The user is NOT authenticated yet — they still need to complete MFA.
    expect(auth.isAuthenticated.value).toBe(false);

    // user.value must remain null.
    expect(auth.user.value).toBeNull();

    // No tokens should be stored.
    // With cookie-based auth, tokens are cleared via server-side cookie expiry.
    // No localStorage assertions needed.
  });

  // ── Test 3 ─────────────────────────────────────────────────────────────────

  it('verifyMfa() completes authentication', async () => {
    /**
     * SCENARIO
     * ────────
     * The user has already received an mfa_token (step 1 of login returned
     * mfa_required: true).  They now submit their TOTP code.
     *
     * useAuth.verifyMfa(mfaToken, totpCode) must:
     *   1. POST to /auth/2fa/login with { mfa_token, totp_code }.
     *   2. Store the tokens returned by the server.
     *   3. Fetch the profile from /users/me.
     *   4. Leave the user authenticated.
     *
     * We do NOT call login() first in this test — we go directly to verifyMfa()
     * because the composable does not require login() to be called first.
     * The mfa_token is passed as a plain string argument.
     */
    vi.stubGlobal(
      'fetch',
      createMockFetch({
        '/auth/2fa/login': {
          status: 200,
          body: MFA_VERIFY_RESPONSE,
        },
        '/users/me': {
          status: 200,
          body: USERS_ME_RESPONSE,
        },
      }),
    );

    const auth = useAuth();

    // Call verifyMfa with the mfa_token from the previous step + the TOTP code.
    await auth.verifyMfa('mfa-tok', '123456');

    // ── Assertions ──────────────────────────────────────────────────────────

    // After MFA verification, the user should be authenticated.
    expect(auth.isAuthenticated.value).toBe(true);

    // The user profile should have been fetched and stored.
    expect(auth.user.value).toEqual(MOCK_USER);

    // MFA flow returns different token values — verify those were stored.
    // With cookie-based auth, MFA tokens are set via httpOnly cookies by the API.
  });

  // ── Test 4 ─────────────────────────────────────────────────────────────────

  it('logout() clears tokens and state', async () => {
    /**
     * SCENARIO
     * ────────
     * Start from an authenticated state (tokens + user in memory) then call
     * logout().
     *
     * useAuth.logout() must:
     *   1. POST to /auth/logout to invalidate the server-side session.
     *   2. Call clearTokens() — removes both token keys from localStorage.
     *   3. Set user.value = null so isAuthenticated becomes false.
     *
     * SET-UP STRATEGY
     * ───────────────
     * We call login() to reach an authenticated state first.  This is more
     * realistic than manually writing to localStorage because it exercises the
     * same code path a real user would take.
     *
     * Note: /auth/logout must be registered in the mock even if it returns
     * nothing useful, because api() always checks response.ok and throws on
     * non-2xx.  useAuth.logout() wraps the call in try/finally, so the
     * cleanup runs even if the server call fails — but it's cleaner to
     * return 200 here to avoid noise.
     */
    vi.stubGlobal(
      'fetch',
      createMockFetch({
        // Needed for the login() setup call
        '/auth/login': {
          status: 200,
          body: LOGIN_SUCCESS_RESPONSE,
        },
        // Needed for fetchProfile() called by login()
        '/users/me': {
          status: 200,
          body: USERS_ME_RESPONSE,
        },
        // The actual endpoint under test
        '/auth/logout': {
          status: 200,
          body: {},
        },
      }),
    );

    const auth = useAuth();

    // First, reach an authenticated state via login().
    await auth.login('a@test.ro', 'password123');

    // Sanity-check: login worked.
    expect(auth.isAuthenticated.value).toBe(true);
    expect(auth.user.value).not.toBeNull();

    // Now call logout() — the action under test.
    await auth.logout();

    // ── Assertions ──────────────────────────────────────────────────────────

    // The user object must be wiped.
    expect(auth.user.value).toBeNull();

    // isAuthenticated is a computed(() => user.value !== null), so it must
    // now be false since user.value is null.
    expect(auth.isAuthenticated.value).toBe(false);

    // Both token keys must have been removed from localStorage.
    // With cookie-based auth, tokens are cleared via server-side cookie expiry.
    // No localStorage assertions needed.
  });

  // ── Test 5 ─────────────────────────────────────────────────────────────────

  it('requireRole() returns false when user has wrong role', async () => {
    /**
     * SCENARIO
     * ────────
     * Verify the role-checking utility works correctly for both matching and
     * non-matching roles.
     *
     * requireRole(...roles) returns true iff:
     *   - user.value is not null, AND
     *   - user.value.role is in the `roles` list
     *
     * We set up a user with role 'student' and assert:
     *   - requireRole('admin', 'teacher') → false  (student is neither)
     *   - requireRole('student')          → true   (exact match)
     *
     * SET-UP STRATEGY
     * ───────────────
     * We log in with a mock that returns a user whose role is 'student'.
     * This is the cleanest way to set user.value without bypassing the
     * composable's internal structure.
     */
    const STUDENT_USER = {
      id: '2',
      schoolId: 's1',
      role: 'student' as const,
      email: 'elev@test.ro',
      firstName: 'Elev',
      lastName: 'Test',
      totpEnabled: false,
    };

    vi.stubGlobal(
      'fetch',
      createMockFetch({
        '/auth/login': {
          status: 200,
          body: LOGIN_SUCCESS_RESPONSE,
        },
        '/users/me': {
          status: 200,
          // Wrap the student user in the { data: ... } envelope.
          body: { data: STUDENT_USER },
        },
      }),
    );

    const auth = useAuth();

    // Log in to populate user.value with the student profile.
    await auth.login('elev@test.ro', 'password123');

    // Confirm the user was set correctly.
    expect(auth.user.value?.role).toBe('student');

    // ── Assertions ──────────────────────────────────────────────────────────

    // A 'student' should NOT satisfy an 'admin' or 'teacher' requirement.
    expect(auth.requireRole('admin', 'teacher')).toBe(false);

    // A 'student' SHOULD satisfy a 'student' requirement.
    expect(auth.requireRole('student')).toBe(true);

    // Boundary: passing no roles at all should also return false (nothing to match).
    // This is implicit — roles.includes(user.value.role) on an empty array is false.
    expect(auth.requireRole()).toBe(false);
  });
});
