/**
 * auth/login.spec.ts
 *
 * End-to-end tests for the CatalogRO login page (/login).
 *
 * WHAT THIS FILE COVERS
 * ─────────────────────
 * This file exercises the full authentication flow for all five user roles:
 *   parent, student, teacher, admin, secretary.
 *
 * It covers:
 *   - Page rendering (form elements are visible, button label is correct)
 *   - HTML5 form validation (empty submit stays on /login)
 *   - Server-side error handling (wrong credentials show an error)
 *   - Successful login for users who do NOT require MFA (parent, student)
 *   - Successful login with TOTP MFA for users who DO require MFA (teacher, admin, secretary)
 *   - MFA failure (wrong 6-digit code shows an error)
 *   - Logout (clicking the logout button clears the session and redirects to /login)
 *
 * TEST STRUCTURE
 * ──────────────
 * Tests 1–9  : raw `test` from @playwright/test — they handle their own login flow
 *              so they need a plain, unauthenticated `page` fixture.
 * Test 10    : `authTest` from our custom fixture — `parentPage` is already logged in,
 *              so the test can jump straight to the logout action.
 *
 * PAGE OBJECT
 * ───────────
 * LoginPage wraps all /login form interactions (fillEmail, fillPassword, submit,
 * fillMfaCode, getErrorMessage, etc.). Using it keeps test code readable and
 * protects us from selector drift in login.vue.
 *
 * CREDENTIALS
 * ───────────
 * All test credentials live in TEST_USERS (auth.fixture.ts) and match seed data
 * in api/db/seed.sql. Password for every user: "catalog2026".
 * TOTP secret for all MFA users: "JBSWY3DPEHPK3PXP".
 */

// ── External: Standard Playwright test runner ─────────────────────────────────
// We import from @playwright/test directly here because tests 1–9 need a plain,
// unauthenticated page. The custom auth fixture pre-logs-in the page before the
// test starts, which would defeat the purpose of these login flow tests.
import { test, expect } from '@playwright/test';

// ── Internal: project-relative helpers and page objects ───────────────────────
// `authTest` is aliased to avoid a name collision with the plain `test` above.
// `parentPage` from this fixture is already on the dashboard when the test begins.
// `generateTOTP` creates a valid 6-digit code from the seeded TOTP secret and
// automatically waits if we are close to a 30-second TOTP window boundary.
// `LoginPage` encapsulates all /login form interactions so tests read like prose.
import { test as authTest, TEST_USERS } from '../fixtures/auth.fixture';
import { generateTOTP } from '../helpers/totp';
import { LoginPage } from '../page-objects/login.page';

// ─────────────────────────────────────────────────────────────────────────────
// TEST SUITE
// ─────────────────────────────────────────────────────────────────────────────

test.describe('login page', () => {
  // ────────────────────────────────────────────────────────────────────────────
  // TEST 1: Page renders correctly
  //
  // PURPOSE: Verify that the three core form elements are present and that the
  // submit button has the correct Romanian label. If any of these fail, every
  // other test in this suite will also fail — so this is our smoke test.
  // ────────────────────────────────────────────────────────────────────────────
  test('login page renders', async ({ page }) => {
    // Create a LoginPage instance. The constructor only wires up locators —
    // it does NOT trigger any network request yet.
    const loginPage = new LoginPage(page);

    // Navigate to /login and wait for the page to load.
    await loginPage.goto();

    // Assert all three form controls are visible in the viewport.
    await expect(loginPage.emailInput).toBeVisible();
    await expect(loginPage.passwordInput).toBeVisible();
    await expect(loginPage.submitButton).toBeVisible();

    // The submit button must display the Romanian text "Autentificare".
    // This catches accidental fallback to an English default or a missing i18n key.
    await expect(loginPage.submitButton).toHaveText('Autentificare');
  });

  // ────────────────────────────────────────────────────────────────────────────
  // TEST 2: Empty form shows browser validation
  //
  // PURPOSE: Submitting an empty form should be blocked by the browser's built-in
  // HTML5 required validation (the `required` attribute on both inputs). The URL
  // must remain /login — no server request is made.
  //
  // NOTE: We assert the URL still contains "/login". We use toContain because the
  // full URL includes protocol + host (e.g. http://localhost:3000/login).
  // ────────────────────────────────────────────────────────────────────────────
  test('empty form shows validation', async ({ page }) => {
    const loginPage = new LoginPage(page);
    await loginPage.goto();

    // Click submit without filling any fields.
    // HTML5 validation should intercept the click and prevent form submission.
    await loginPage.submit();

    // The browser should NOT have navigated away — we are still on /login.
    expect(page.url()).toContain('/login');
  });

  // ────────────────────────────────────────────────────────────────────────────
  // TEST 3: Invalid credentials show a server-side error message
  //
  // PURPOSE: When the email/password pair does not match any account, the API
  // returns an error and the Vue component renders the login-error element.
  // This test confirms that the error UI actually appears (not just that the
  // URL stays at /login).
  // ────────────────────────────────────────────────────────────────────────────
  test('invalid credentials show error', async ({ page }) => {
    const loginPage = new LoginPage(page);
    await loginPage.goto();

    // Enter credentials that do not exist in the seed database.
    await loginPage.fillEmail('wrong@test.ro');
    await loginPage.fillPassword('wrongpass');
    await loginPage.submit();

    // The error div is rendered with v-if="error", so it only appears in the
    // DOM when the API response sets the error ref. We wait for it to become
    // visible rather than asserting immediately (avoids flakiness on slow CI).
    await expect(loginPage.loginError).toBeVisible();
  });

  // ────────────────────────────────────────────────────────────────────────────
  // TEST 4: Parent login succeeds (no MFA required)
  //
  // PURPOSE: Parents do NOT require two-factor authentication. After entering
  // valid credentials and clicking submit, the user should land on '/' (dashboard).
  //
  // ROLE: ion.moldovan@gmail.com — parent of Andrei Moldovan (class 2A).
  // ────────────────────────────────────────────────────────────────────────────
  test('parent login succeeds without MFA', async ({ page }) => {
    const loginPage = new LoginPage(page);
    await loginPage.goto();

    // Fill credentials from the shared TEST_USERS object.
    // Using TEST_USERS keeps credentials DRY — one change in auth.fixture.ts
    // updates every test automatically.
    await loginPage.fillEmail(TEST_USERS.parent.email);
    await loginPage.fillPassword(TEST_USERS.parent.password);
    await loginPage.submit();

    // Wait for the Nuxt router to navigate to the dashboard root.
    // A 10-second timeout gives the API enough time even on cold-start CI.
    await page.waitForURL('/', { timeout: 10_000 });

    // Double-check via the LoginPage helper that we are on the dashboard.
    expect(loginPage.isOnDashboard()).toBe(true);
  });

  // ────────────────────────────────────────────────────────────────────────────
  // TEST 5: Student login succeeds (no MFA required)
  //
  // PURPOSE: Students also do NOT require two-factor authentication.
  // Same flow as Test 4 but with student credentials.
  //
  // ROLE: andrei.moldovan@elev.rebreanu.ro — student in class 2A.
  // ────────────────────────────────────────────────────────────────────────────
  test('student login succeeds without MFA', async ({ page }) => {
    const loginPage = new LoginPage(page);
    await loginPage.goto();

    await loginPage.fillEmail(TEST_USERS.student.email);
    await loginPage.fillPassword(TEST_USERS.student.password);
    await loginPage.submit();

    // Student has mfaRequired: false — should redirect directly to dashboard.
    await page.waitForURL('/', { timeout: 10_000 });
    expect(loginPage.isOnDashboard()).toBe(true);
  });

  // ────────────────────────────────────────────────────────────────────────────
  // TEST 6: Teacher login shows MFA step, then succeeds with valid TOTP
  //
  // PURPOSE: Teachers MUST complete two-factor authentication. After the correct
  // password, the API returns mfaRequired: true, and the Vue component swaps the
  // password form for a TOTP input. Entering a valid code logs them in.
  //
  // ROLE: ana.dumitrescu@scoala-rebreanu.ro — primary teacher, class 2A.
  // ────────────────────────────────────────────────────────────────────────────
  test('teacher login shows MFA then succeeds', async ({ page }) => {
    const loginPage = new LoginPage(page);
    await loginPage.goto();

    // Step 1: Submit email + password credentials.
    await loginPage.fillEmail(TEST_USERS.teacher.email);
    await loginPage.fillPassword(TEST_USERS.teacher.password);
    await loginPage.submit();

    // Step 2: Wait for the MFA input to appear.
    // The API replies with mfaRequired: true, which triggers the Vue v-else block
    // that hides the password form and shows the TOTP form instead.
    await page.getByTestId('mfa-input').waitFor({ state: 'visible' });

    // Step 3: Generate a fresh TOTP code. The helper automatically waits if
    // we are within 5 seconds of a window boundary to avoid race conditions.
    const code = await generateTOTP();

    // Step 4: Fill the TOTP code and submit the MFA form.
    // The MFA form has its own submit button ('mfa-submit-button'), separate
    // from the login form's 'submit-button'.
    await loginPage.fillMfaCode(code);
    await loginPage.submitMfa();

    // Step 5: The API validates the code and issues a session. Nuxt navigates to '/'.
    await page.waitForURL('/', { timeout: 10_000 });
    expect(loginPage.isOnDashboard()).toBe(true);
  });

  // ────────────────────────────────────────────────────────────────────────────
  // TEST 7: Teacher login with invalid TOTP shows MFA error
  //
  // PURPOSE: When a user enters a wrong 6-digit code (or a placeholder like
  // "000000"), the API should reject it and the mfa-error element should appear.
  //
  // ROLE: ana.dumitrescu@scoala-rebreanu.ro — same teacher as Test 6.
  // ────────────────────────────────────────────────────────────────────────────
  test('teacher login with invalid TOTP shows error', async ({ page }) => {
    const loginPage = new LoginPage(page);
    await loginPage.goto();

    // Reach the MFA step with valid password credentials.
    await loginPage.fillEmail(TEST_USERS.teacher.email);
    await loginPage.fillPassword(TEST_USERS.teacher.password);
    await loginPage.submit();

    // Wait for the MFA input to appear before we try to fill it.
    await page.getByTestId('mfa-input').waitFor({ state: 'visible' });

    // Enter an obviously wrong code — "000000" is never a valid TOTP code
    // (the odds of it matching are 1-in-a-million, and OTP libs reject it if
    // the counter doesn't align).
    await loginPage.fillMfaCode('000000');
    await loginPage.submitMfa();

    // The mfa-error element is in the TOTP section (v-else block in login.vue).
    // It uses a separate testid from login-error to avoid ambiguity.
    await expect(loginPage.mfaError).toBeVisible();
  });

  // ────────────────────────────────────────────────────────────────────────────
  // TEST 8: Admin login with MFA succeeds
  //
  // PURPOSE: School admin (director) also requires TOTP. This test duplicates
  // the teacher MFA flow to confirm that admin-role accounts go through the same
  // path and land on the dashboard after a valid code.
  //
  // ROLE: director@scoala-rebreanu.ro — Maria Popescu, school director.
  // ────────────────────────────────────────────────────────────────────────────
  test('admin login with MFA succeeds', async ({ page }) => {
    const loginPage = new LoginPage(page);
    await loginPage.goto();

    // Step 1: Authenticate with email + password.
    await loginPage.fillEmail(TEST_USERS.admin.email);
    await loginPage.fillPassword(TEST_USERS.admin.password);
    await loginPage.submit();

    // Step 2: Wait for the MFA prompt to appear.
    await page.getByTestId('mfa-input').waitFor({ state: 'visible' });

    // Step 3: Generate and submit a valid TOTP code.
    const code = await generateTOTP();
    await loginPage.fillMfaCode(code);
    await loginPage.submitMfa();

    // Step 4: Assert the admin landed on the dashboard.
    await page.waitForURL('/', { timeout: 10_000 });
    expect(loginPage.isOnDashboard()).toBe(true);
  });

  // ────────────────────────────────────────────────────────────────────────────
  // TEST 9: Secretary login with MFA succeeds
  //
  // PURPOSE: Secretary role also requires TOTP. Same flow as admin (Test 8)
  // to confirm that secretary-role accounts also complete MFA correctly.
  //
  // ROLE: secretar@scoala-rebreanu.ro — Elena Ionescu, school secretary.
  // ────────────────────────────────────────────────────────────────────────────
  test('secretary login with MFA succeeds', async ({ page }) => {
    const loginPage = new LoginPage(page);
    await loginPage.goto();

    // Step 1: Authenticate with email + password.
    await loginPage.fillEmail(TEST_USERS.secretary.email);
    await loginPage.fillPassword(TEST_USERS.secretary.password);
    await loginPage.submit();

    // Step 2: Wait for the MFA prompt.
    await page.getByTestId('mfa-input').waitFor({ state: 'visible' });

    // Step 3: Generate and submit a valid TOTP code.
    const code = await generateTOTP();
    await loginPage.fillMfaCode(code);
    await loginPage.submitMfa();

    // Step 4: Assert the secretary landed on the dashboard.
    await page.waitForURL('/', { timeout: 10_000 });
    expect(loginPage.isOnDashboard()).toBe(true);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// TEST 10: Logout clears the session and redirects to /login
//
// WHY THIS IS OUTSIDE THE DESCRIBE BLOCK (technically it is inside — see below)
// NOTE: This test uses `authTest` (the custom fixture) instead of `test`.
// `authTest.describe` is functionally identical to `test.describe` but it
// carries the extended fixture definitions (adminPage, parentPage, etc.).
//
// We use `parentPage` because parents have no MFA and their login is the
// fastest way to get an authenticated session as a setup step.
//
// PURPOSE: After a successful login, clicking the logout button must:
//   1. Call the logout API endpoint (or clear the local session state)
//   2. Redirect the browser back to /login
//   3. Leave the user unable to access protected routes
// ─────────────────────────────────────────────────────────────────────────────
authTest.describe('login page — authenticated actions', () => {
  authTest('logout clears session and redirects to login', async ({ parentPage }) => {
    // `parentPage` is a Playwright Page that auth.fixture.ts has already logged
    // in as Ion Moldovan (parent). The fixture called performLogin() before this
    // test function ran, so we start on the dashboard at '/'.

    // The logout button lives in the global app layout (not on the login page),
    // so we query it directly on the page rather than through LoginPage.
    // data-testid="logout-button" must be present in the layout component
    // (e.g. web/layouts/default.vue or web/components/AppHeader.vue).
    const logoutButton = parentPage.getByTestId('logout-button');

    // Click the logout button. This should:
    //   - Invalidate the session (clear JWT cookie / Pinia auth state)
    //   - Trigger a Nuxt navigation back to /login
    await logoutButton.click();

    // Wait for the redirect to complete. A 10-second timeout is generous but
    // accounts for any session invalidation round-trips.
    await parentPage.waitForURL('**/login', { timeout: 10_000 });

    // Final assertion: the URL must contain "/login", confirming the user was
    // redirected to the public login page and is no longer on a protected route.
    expect(parentPage.url()).toContain('/login');
  });
});
