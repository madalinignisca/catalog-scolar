/**
 * @vitest-environment nuxt
 *
 * Force this test file to run ONLY in the Nuxt environment (not the plain
 * happy-dom environment).  When `@nuxt/test-utils` is used as the config
 * provider it spins up two Vitest workers: one with the full Nuxt runtime
 * (which has `useRuntimeConfig`, composable auto-imports, etc.) and one with
 * bare happy-dom.  The `lib/api.ts` module calls `useRuntimeConfig()` via
 * `getApiBase()` whenever `import.meta.client` is true — that call requires
 * the Nuxt instance to be present.  Without this annotation every test would
 * run twice, and the happy-dom run would fail with "[nuxt] instance
 * unavailable".  By pinning to the nuxt environment we ensure all 6 tests run
 * in the environment where the Nuxt runtime (and our stubs in setup.ts) is
 * fully initialised.
 */

/**
 * Tests for lib/api.ts — the typed fetch wrapper used by the CatalogRO frontend.
 *
 * WHAT IS TESTED
 * ──────────────
 * 1. A normal GET request returns the parsed response body.
 * 2. A POST request serialises the body to JSON and sends it.
 * 3. A 401 response triggers a token-refresh round-trip, then retries the
 *    original request with the new access token.
 * 4. When the refresh endpoint itself fails the stored tokens are cleared so
 *    the user is effectively logged out.
 * 5. Non-OK responses throw an `ApiError` with the correct HTTP status, machine
 *    code, and human-readable message taken from the response body.
 * 6. The `skipAuth` option prevents the Authorization header from being added,
 *    even when a token is present in localStorage.
 *
 * HOW FETCH IS MOCKED
 * ───────────────────
 * `lib/api.ts` calls the native browser `fetch()` directly (not Nuxt's $fetch).
 * We replace the global `fetch` with a spy using `vi.stubGlobal('fetch', ...)`.
 * The spy is wrapped around `createMockFetch()` (from test/helpers/mock-api.ts)
 * which produces a realistic `Response` object from a simple route-→-body table.
 * For tests that need different responses on successive calls (test 3) we use
 * `vi.fn().mockResolvedValueOnce(...)` to sequence return values.
 *
 * WHY `import.meta.client` IS NOT A PROBLEM HERE
 * ───────────────────────────────────────────────
 * `vitest.config.ts` sets  `define: { 'import.meta.client': true }`, which
 * makes Vite replace every occurrence of the identifier with the literal `true`
 * at build time.  That means the guards in api.ts (e.g. `if (!import.meta.client)
 * return null`) are compiled away as dead code, and localStorage is always
 * accessible — exactly as it would be in a real browser.
 *
 * TOKEN STORAGE KEYS (must match lib/api.ts)
 * ──────────────────────────────────────────
 * Access token  → 'catalogro_access_token'
 * Refresh token → 'catalogro_refresh_token'
 */

// ---------------------------------------------------------------------------
// Imports
// ---------------------------------------------------------------------------

/**
 * `createMockFetch`   — factory that returns a fetch-compatible function
 *                       backed by a route-→-response map.
 * `mockSuccessResponse` — wraps a payload in `{ data: <payload> }` so test
 *                       assertions can read `response.data`.
 * `mockApiError`      — builds the `{ error: { code, message } }` body shape
 *                       that the real API server (and ApiError) expect.
 *
 * Relative imports must precede alias imports (`~/…`) to satisfy the
 * import-x/order ESLint rule configured in this project.
 */
import { createMockFetch, mockSuccessResponse, mockApiError } from '../helpers/mock-api';

/**
 * We import the functions we want to test directly from the source module.
 * `api`          — the main fetch wrapper (the subject of all 6 tests).
 * `ApiError`     — the custom error class thrown on non-OK responses.
 * `setTokens`    — helper to pre-populate localStorage before tests that need
 *                  an existing access/refresh token.
 * `getAccessToken` — used in assertions to verify the token was NOT cleared
 *                  when it shouldn't have been (sanity checks).
 */
import { api, ApiError, setTokens } from '~/lib/api';

// ---------------------------------------------------------------------------
// Constants shared across tests
// ---------------------------------------------------------------------------

/**
 * The API base URL that `getApiBase()` returns in the test environment.
 * Since import.meta.client is true in tests, getApiBase() returns the
 * relative path '/api/v1' (same-origin proxy). Mock fetch routes must
 * use this prefix to match the URLs that api() will request.
 */
const API_BASE = '/api/v1';

/**
 * localStorage key names — copied from lib/api.ts (they are module-private
 * there, so we duplicate them here for test setup/assertion purposes).
 */
const ACCESS_TOKEN_KEY = 'catalogro_access_token';
const REFRESH_TOKEN_KEY = 'catalogro_refresh_token';

// ---------------------------------------------------------------------------
// Test suite
// ---------------------------------------------------------------------------

describe('api() — typed fetch wrapper', () => {
  // ─────────────────────────────────────────────────────────────────────────
  // Test 1: Successful GET request
  // ─────────────────────────────────────────────────────────────────────────

  it('successful GET request', async () => {
    /**
     * ARRANGE
     * Install a mock fetch that responds to GET /users/me with a 200 and a
     * body containing `{ data: { id: '1' } }`.
     * `mockSuccessResponse` wraps the payload in the standard envelope shape
     * `{ data: <payload> }` matching the type parameter the caller passes to
     * api<T>().
     */
    vi.stubGlobal(
      'fetch',
      createMockFetch({
        '/users/me': {
          status: 200,
          body: mockSuccessResponse({ id: '1' }),
        },
      }),
    );

    // ACT — call the api() wrapper for the /users/me path (no auth needed for
    // this test since localStorage is empty after the global beforeEach clear).
    const response = await api<{ data: { id: string } }>('/users/me');

    /**
     * ASSERT
     * api() unwraps the { data: ... } envelope and returns the inner payload
     * directly. So { data: { id: '1' } } from the server becomes just
     * { id: '1' } after unwrapping (no snake_case keys to convert here).
     */
    expect(response).toEqual({ id: '1' });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // Test 2: Successful POST with body
  // ─────────────────────────────────────────────────────────────────────────

  it('successful POST with body', async () => {
    /**
     * ARRANGE
     * For this test we care about WHAT was sent to fetch, not just the return
     * value.  We therefore wrap createMockFetch inside a vi.fn() spy so we can
     * later inspect the `init` argument (method, headers, body) that api()
     * passed to fetch.
     */
    const mockFetch = vi.fn(
      createMockFetch({
        '/auth/login': {
          status: 200,
          // The login endpoint normally returns tokens; shape doesn't matter
          // for this test, so we use a minimal object.
          body: { access_token: 'tok', refresh_token: 'ref' },
        },
      }),
    );

    vi.stubGlobal('fetch', mockFetch);

    // The credentials object that the caller would pass as `body`.
    const credentials = { email: 'a@b.com', password: 'p' };

    // ACT — POST request with a structured body.
    await api('/auth/login', {
      method: 'POST',
      body: credentials,
      skipAuth: true, // skip auth header so we don't need to set up a token
    });

    /**
     * ASSERT
     * fetch() should have been called exactly once.
     * The second argument (RequestInit) should contain:
     *   - method: 'POST'
     *   - body:   the credentials serialised as JSON
     *
     * `mockFetch.mock.calls[0]` gives us the arguments from the first call.
     * Index [0] is the URL string, index [1] is the RequestInit object.
     */
    expect(mockFetch).toHaveBeenCalledTimes(1);

    const [calledUrl, calledInit] = mockFetch.mock.calls[0] as [string, RequestInit];

    // Verify the full URL was constructed correctly.
    expect(calledUrl).toBe(`${API_BASE}/auth/login`);

    // Verify POST method.
    expect(calledInit.method).toBe('POST');

    // Verify the body was JSON-serialised.
    expect(calledInit.body).toBe(JSON.stringify(credentials));
  });

  // ─────────────────────────────────────────────────────────────────────────
  // Test 3: 401 triggers token refresh and retry
  // ─────────────────────────────────────────────────────────────────────────

  it('401 triggers token refresh and retry', async () => {
    /**
     * ARRANGE
     * This test requires the fetch mock to return DIFFERENT responses for the
     * SAME URL on successive calls:
     *
     *   Call 1 → GET /users/me → 401 Unauthorized   (token is expired)
     *   Call 2 → POST /auth/refresh → 200 OK         (new tokens issued)
     *   Call 3 → GET /users/me → 200 OK              (retry with new token)
     *
     * `createMockFetch` always returns the same response for a route, so we
     * cannot use it alone here.  Instead we build a manual vi.fn() that uses
     * `mockResolvedValueOnce` to queue up responses in order.
     */

    // The body that the server returns on the successful retry of /users/me.
    const userPayload = mockSuccessResponse({ id: '1', name: 'Ion Popescu' });

    // The body that /auth/refresh returns when it issues new tokens.
    const refreshPayload = {
      access_token: 'new-access-token',
      refresh_token: 'new-refresh-token',
    };

    /**
     * Helper: build a fake Response object with the given status and JSON body.
     * This mirrors what the real fetch() / createMockFetch produces so our
     * code under test can call .ok, .status, and .json() on it.
     */
    const makeResponse = (status: number, body: unknown): Response =>
      new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
      });

    /**
     * Queue up the three responses in the exact order api() will request them:
     *
     *  1st call  — GET /users/me   → 401 (triggers refresh logic)
     *  2nd call  — POST /auth/refresh → 200 with new tokens
     *  3rd call  — GET /users/me   → 200 with user payload (the retry)
     */
    const mockFetch = vi
      .fn()
      .mockResolvedValueOnce(makeResponse(401, { error: { message: 'Unauthorized' } }))
      .mockResolvedValueOnce(makeResponse(200, refreshPayload))
      .mockResolvedValueOnce(makeResponse(200, userPayload));

    vi.stubGlobal('fetch', mockFetch);

    /**
     * Pre-populate localStorage with an expired access token and a valid
     * refresh token.  The api() function reads the access token to set the
     * Authorization header, and tryRefreshToken() reads the refresh token to
     * send to /auth/refresh.
     *
     * We use setTokens() (re-exported from lib/api.ts) so we stay consistent
     * with the real token storage logic.
     */
    setTokens('expired-access-token', 'valid-refresh-token');

    // ACT — make the request that will fail with 401 first, then succeed.
    const result = await api<{ data: { id: string; name: string } }>('/users/me');

    /**
     * ASSERT 1 — the eventual result is the user payload from the retry call.
     * api() unwraps the { data: ... } envelope and applies snakeToCamel, so the
     * result is the inner object directly (no snake_case keys in this payload,
     * so no key changes occur here).
     */
    expect(result).toEqual({ id: '1', name: 'Ion Popescu' });

    /**
     * ASSERT 2 — fetch was called exactly 3 times (original + refresh + retry).
     */
    expect(mockFetch).toHaveBeenCalledTimes(3);

    /**
     * ASSERT 3 — the second call went to /auth/refresh.
     * mock.calls[1][0] is the URL of the 2nd fetch() invocation.
     */
    expect(mockFetch.mock.calls[1][0]).toBe(`${API_BASE}/auth/refresh`);

    /**
     * ASSERT 4 — after a successful refresh, the new tokens are stored in
     * localStorage so the third (retry) request carries the new access token.
     */
    expect(localStorage.getItem(ACCESS_TOKEN_KEY)).toBe('new-access-token');
    expect(localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('new-refresh-token');

    /**
     * ASSERT 5 — the retry (3rd call) used the new access token in the
     * Authorization header.
     */
    const retryInit = mockFetch.mock.calls[2][1] as RequestInit;
    expect((retryInit.headers as Record<string, string>)['Authorization']).toBe(
      'Bearer new-access-token',
    );
  });

  // ─────────────────────────────────────────────────────────────────────────
  // Test 4: Refresh failure clears tokens
  // ─────────────────────────────────────────────────────────────────────────

  it('refresh failure clears tokens', async () => {
    /**
     * ARRANGE
     * Simulate the scenario where:
     *   1. The request to /users/me returns 401.
     *   2. The attempt to refresh tokens returns a non-OK response (e.g. 401
     *      meaning the refresh token itself has also expired).
     *
     * In this case api.ts should call clearTokens() and NOT retry the original
     * request (since there is nothing to retry with).
     */
    const makeResponse = (status: number, body: unknown): Response =>
      new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
      });

    // 1st call: original request fails with 401.
    // 2nd call: refresh endpoint also fails with 401.
    const mockFetch = vi
      .fn()
      .mockResolvedValueOnce(
        makeResponse(401, { error: { code: 'TOKEN_EXPIRED', message: 'Token expired' } }),
      )
      .mockResolvedValueOnce(
        makeResponse(401, { error: { code: 'REFRESH_INVALID', message: 'Refresh token invalid' } }),
      );

    vi.stubGlobal('fetch', mockFetch);

    /**
     * Also stub window.location.href assignment so the redirect that follows a
     * failed refresh does not throw in the happy-dom environment.
     * (happy-dom implements location but assigning .href can cause navigation
     * side-effects that are irrelevant to this test.)
     */
    const locationDescriptor = Object.getOwnPropertyDescriptor(window, 'location');
    /**
     * We cannot spread a `window.location` object directly because it is a
     * class instance (Location) and spreading class instances drops prototype
     * methods — the @typescript-eslint/no-misused-spread rule forbids it.
     * Instead we build a plain object with only the properties our code
     * actually reads (`href`), which is all that is needed here.
     */
    Object.defineProperty(window, 'location', {
      value: { href: '' },
      writable: true,
      configurable: true,
    });

    // Pre-populate tokens — they should be gone after the failed refresh.
    setTokens('old-access-token', 'old-refresh-token');

    // Confirm the tokens are actually in localStorage before the test action.
    expect(localStorage.getItem(ACCESS_TOKEN_KEY)).toBe('old-access-token');
    expect(localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('old-refresh-token');

    // ACT — this call will hit 401, attempt refresh (which also fails), and
    // then clearTokens().  Because the refresh itself returns a non-ok
    // response, tryRefreshToken() returns false and api() does NOT retry — so
    // the function does NOT throw an ApiError at the original URL.  Instead it
    // clears tokens and redirects.  The response after the failed refresh path
    // falls through to the `if (!response.ok)` check for the original 401
    // response, which DOES throw an ApiError.
    //
    // We therefore expect the call to reject with an ApiError (status 401).
    // We catch it to avoid an unhandled-rejection failure.
    try {
      await api('/users/me');
    } catch {
      // We expect an error here — swallow it, we only care about side-effects.
    }

    /**
     * ASSERT — both tokens must be cleared from localStorage.
     */
    expect(localStorage.getItem(ACCESS_TOKEN_KEY)).toBeNull();
    expect(localStorage.getItem(REFRESH_TOKEN_KEY)).toBeNull();

    // Restore window.location to its original descriptor if it existed.
    if (locationDescriptor) {
      Object.defineProperty(window, 'location', locationDescriptor);
    }
  });

  // ─────────────────────────────────────────────────────────────────────────
  // Test 5: ApiError has correct structure
  // ─────────────────────────────────────────────────────────────────────────

  it('ApiError has correct structure', async () => {
    /**
     * ARRANGE
     * Mock a 422 Unprocessable Entity response with an error body that
     * contains both a machine-readable `code` and a human-readable `message`.
     *
     * This is the standard error shape the CatalogRO API server emits (defined
     * by the `ApiErrorBody` interface in lib/api.ts).
     */
    vi.stubGlobal(
      'fetch',
      createMockFetch({
        '/users/me': {
          status: 422,
          body: mockApiError(422, 'VALIDATION', 'Invalid email'),
        },
      }),
    );

    // ACT + ASSERT — we expect api() to throw, so we use a try/catch and then
    // make assertions about the thrown value.
    let thrownError: unknown;

    try {
      // No token in localStorage (cleared by beforeEach), and we are not
      // passing skipAuth — the Authorization header simply won't be set.
      await api('/users/me');
    } catch (err) {
      thrownError = err;
    }

    /**
     * ASSERT 1 — something was actually thrown (guard against silently passing
     * if the mock accidentally returns a 200).
     */
    expect(thrownError).toBeDefined();

    /**
     * ASSERT 2 — the thrown value is an instance of our custom ApiError class,
     * not a plain Error or a generic fetch rejection.
     */
    expect(thrownError).toBeInstanceOf(ApiError);

    // Cast to ApiError so TypeScript lets us access the custom properties.
    const apiError = thrownError as ApiError;

    /**
     * ASSERT 3 — HTTP status code is carried on the error object.
     * Useful for UI code that wants to show different messages for 404 vs 422.
     */
    expect(apiError.status).toBe(422);

    /**
     * ASSERT 4 — machine-readable code is preserved exactly as sent by the
     * server.  Allows programmatic handling (e.g. switch on apiError.code).
     */
    expect(apiError.code).toBe('VALIDATION');

    /**
     * ASSERT 5 — the human-readable message becomes the Error's `.message`
     * property (set via `super(message)` in the ApiError constructor).
     */
    expect(apiError.message).toBe('Invalid email');

    /**
     * ASSERT 6 — the `name` property is set to 'ApiError' so that
     * `error.name === 'ApiError'` works as a discriminator without instanceof.
     */
    expect(apiError.name).toBe('ApiError');
  });

  // ─────────────────────────────────────────────────────────────────────────
  // Test 6: skipAuth option omits Authorization header
  // ─────────────────────────────────────────────────────────────────────────

  it('skipAuth option omits Authorization header', async () => {
    /**
     * ARRANGE
     * We need a spy on the mock fetch so we can inspect the `headers` that
     * api() passed in the RequestInit.  We wrap createMockFetch in vi.fn() for
     * the same reason as in test 2.
     */
    const mockFetch = vi.fn(
      createMockFetch({
        '/auth/login': {
          status: 200,
          body: { access_token: 'tok', refresh_token: 'ref' },
        },
      }),
    );

    vi.stubGlobal('fetch', mockFetch);

    /**
     * Pre-populate an access token in localStorage.
     * Without the `skipAuth` flag, api() would include this as a Bearer token
     * in the Authorization header.  With the flag it must NOT appear.
     */
    setTokens('my-access-token', 'my-refresh-token');

    // ACT — call api() with skipAuth: true.
    await api('/auth/login', {
      method: 'POST',
      body: { email: 'a@b.com', password: 'p' },
      skipAuth: true,
    });

    /**
     * ASSERT — fetch was called exactly once, and the RequestInit headers do
     * NOT include an Authorization key.
     *
     * We extract `headers` from the second argument of the first fetch() call.
     * api.ts spreads the headers as a plain Record<string, string>, so we can
     * check for the key directly.
     */
    expect(mockFetch).toHaveBeenCalledTimes(1);

    const calledInit = mockFetch.mock.calls[0][1] as RequestInit;
    const calledHeaders = calledInit.headers as Record<string, string>;

    // The Authorization header must be absent — even though a token exists.
    expect(calledHeaders['Authorization']).toBeUndefined();

    // Sanity-check: Content-Type should still be present (api() always sets it).
    expect(calledHeaders['Content-Type']).toBe('application/json');
  });
});
