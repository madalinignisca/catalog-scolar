/**
 * login.page.ts
 *
 * Page Object Model (POM) for the CatalogRO login page (/login).
 *
 * WHAT IS A PAGE OBJECT?
 * ──────────────────────
 * A Page Object is a class that wraps a specific page (or component) of the
 * application and exposes high-level methods instead of raw Playwright locator
 * calls. This gives you two big benefits:
 *
 *   1. Readability — test code reads like plain English:
 *        await loginPage.fillEmail('user@school.ro');
 *        await loginPage.submit();
 *
 *   2. Maintainability — if the login page HTML changes (e.g. a testid is
 *      renamed), you fix it in ONE place (this file) instead of every test.
 *
 * SELECTOR STRATEGY
 * ─────────────────
 * We use `page.getByTestId(...)` throughout. This resolves to the HTML
 * attribute `data-testid="..."`, which is:
 *   - Independent of CSS classes (Tailwind classes change often)
 *   - Independent of text content (text is Romanian and may be translated)
 *   - Explicit — the developer declares "this element is testable"
 *
 * The data-testid attributes are added in web/pages/login.vue.
 *
 * USAGE
 * ─────
 * import { LoginPage } from '../page-objects/login.page';
 *
 * test('shows error on bad password', async ({ page }) => {
 *   const loginPage = new LoginPage(page);
 *   await loginPage.goto();
 *   await loginPage.fillEmail('wrong@school.ro');
 *   await loginPage.fillPassword('wrongpass');
 *   await loginPage.submit();
 *   const error = await loginPage.getErrorMessage();
 *   expect(error).toContain('eșuată');
 * });
 */

import type { Locator, Page } from '@playwright/test';

/**
 * LoginPage
 *
 * Encapsulates all interactions with the /login route.
 * Covers both the initial email+password form and the MFA (TOTP) step.
 */
export class LoginPage {
  /** The raw Playwright Page object. Stored so methods can act on it. */
  private readonly page: Page;

  // ── Locators ───────────────────────────────────────────────────────────────
  // Locators are defined as class properties so they are created once and
  // reused across method calls. Playwright locators are lazy — they don't
  // actually query the DOM until you call an action (.fill, .click) or
  // assertion (.toBeVisible) on them.

  /**
   * The email address input field on the login form.
   * Maps to: <input data-testid="email-input" type="email" ...>
   */
  readonly emailInput: Locator;

  /**
   * The password input field on the login form.
   * Maps to: <input data-testid="password-input" type="password" ...>
   */
  readonly passwordInput: Locator;

  /**
   * The "Autentificare" submit button on the login form.
   * Maps to: <button data-testid="submit-button" type="submit" ...>
   */
  readonly submitButton: Locator;

  /**
   * The TOTP (6-digit code) input field shown after password verification.
   * Only visible when mfaRequired = true in the Vue component.
   * Maps to: <input data-testid="mfa-input" type="text" ...>
   */
  readonly mfaInput: Locator;

  /**
   * The error message div shown when login fails (wrong password, locked account, etc.).
   * Maps to: <div data-testid="login-error" ...>
   * NOTE: This element is only rendered when `error` ref is non-empty (v-if="error").
   */
  readonly loginError: Locator;

  /**
   * The error message div shown when MFA verification fails (wrong code, expired, etc.).
   * Maps to: <div data-testid="mfa-error" ...>
   * NOTE: Both login-error and mfa-error share the same `error` ref in the Vue component
   * but are in different form sections (v-if="!mfaRequired" vs v-else), so they have
   * distinct testids to make assertions unambiguous.
   */
  readonly mfaError: Locator;

  // ── Constructor ────────────────────────────────────────────────────────────
  /**
   * @param page - The Playwright Page instance injected by the test or fixture.
   *               Each test gets its own isolated browser context, so there is
   *               no state leakage between tests.
   */
  constructor(page: Page) {
    this.page = page;

    // Initialise locators. getByTestId('x') is shorthand for
    // page.locator('[data-testid="x"]').
    this.emailInput = page.getByTestId('email-input');
    this.passwordInput = page.getByTestId('password-input');
    this.submitButton = page.getByTestId('submit-button');
    this.mfaInput = page.getByTestId('mfa-input');
    this.loginError = page.getByTestId('login-error');
    this.mfaError = page.getByTestId('mfa-error');
  }

  // ── Navigation ─────────────────────────────────────────────────────────────
  /**
   * goto
   *
   * Navigates the browser to the login page.
   * baseURL is defined in playwright.config.ts, so this resolves to
   * http://localhost:3000/login.
   *
   * Waits for the page to be fully loaded (DOM + network idle) before returning.
   */
  async goto(): Promise<void> {
    // page.goto returns when the page's load event fires.
    await this.page.goto('/login');
  }

  // ── Form interactions ──────────────────────────────────────────────────────
  /**
   * fillEmail
   *
   * Types an email address into the email input field.
   * Clears any previously typed value first (fill() replaces the field contents).
   *
   * @param email - A valid email string, e.g. 'profesor@scoala-test.ro'
   */
  async fillEmail(email: string): Promise<void> {
    // fill() clears the field and types the new value in one action.
    await this.emailInput.fill(email);
  }

  /**
   * fillPassword
   *
   * Types a password into the password input field.
   *
   * @param password - The plaintext password string.
   */
  async fillPassword(password: string): Promise<void> {
    await this.passwordInput.fill(password);
  }

  /**
   * submit
   *
   * Clicks the submit button to trigger the login request.
   * After this call, the page will either:
   *   - Navigate to '/' (success, no MFA)
   *   - Show the MFA form (success with mfaRequired=true)
   *   - Show an error message (bad credentials)
   */
  async submit(): Promise<void> {
    await this.submitButton.click();
  }

  /**
   * fillMfaCode
   *
   * Types a 6-digit TOTP code into the MFA input field.
   * Call this after submit() when the MFA step is visible.
   *
   * @param code - A 6-character numeric string, e.g. '123456'
   */
  async fillMfaCode(code: string): Promise<void> {
    await this.mfaInput.fill(code);
  }

  // ── Assertions / state queries ─────────────────────────────────────────────
  /**
   * getErrorMessage
   *
   * Returns the text content of the login error div, or null if the error
   * element is not currently visible.
   *
   * Use this in tests to assert that a specific error message appears after
   * a failed login attempt.
   *
   * @returns The trimmed text content of the error element, or null.
   */
  async getErrorMessage(): Promise<string | null> {
    // isVisible() returns false if the element doesn't exist in the DOM
    // (v-if removes it entirely when error is empty in Vue).
    const isVisible = await this.loginError.isVisible();
    if (!isVisible) {
      return null;
    }
    // textContent() returns the raw text (including whitespace).
    // We trim it for consistent assertion comparisons.
    const text = await this.loginError.textContent();
    return text !== null ? text.trim() : null;
  }

  /**
   * getMfaErrorMessage
   *
   * Same as getErrorMessage() but for the MFA step error.
   * Returns null if the MFA error element is not visible.
   *
   * @returns The trimmed text content of the MFA error element, or null.
   */
  async getMfaErrorMessage(): Promise<string | null> {
    const isVisible = await this.mfaError.isVisible();
    if (!isVisible) {
      return null;
    }
    const text = await this.mfaError.textContent();
    return text !== null ? text.trim() : null;
  }

  /**
   * isOnDashboard
   *
   * Returns true if the browser has navigated away from /login to the
   * dashboard (the root path '/').
   *
   * Used to assert that a successful login redirected the user correctly.
   *
   * @returns true if the current URL is the root path, false otherwise.
   */
  isOnDashboard(): boolean {
    // page.url() returns the full URL including protocol and host.
    // We check that the pathname is '/' (dashboard root).
    const url = new URL(this.page.url());
    return url.pathname === '/';
  }
}
