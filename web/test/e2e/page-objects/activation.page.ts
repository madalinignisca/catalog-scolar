/**
 * activation.page.ts
 *
 * Page Object Model (POM) for the CatalogRO account activation page
 * (/activate/[token]).
 *
 * WHAT THIS PAGE DOES
 * ───────────────────
 * New users (teachers, parents, students) do NOT self-register. Their accounts
 * are provisioned by a secretary or admin, then they receive an activation link
 * via email or SMS. This page is that link's destination.
 *
 * On arriving at /activate/[token] the user:
 *   1. Sees their pre-filled identity confirmed (name, role, school)
 *   2. Sets their password (with confirmation field)
 *   3. If the user is a parent, accepts the GDPR consent checkbox
 *   4. Submits — on success they are redirected to the login page
 *
 * The page can also show an error (expired/invalid token) before the form
 * is rendered.
 *
 * SELECTOR STRATEGY
 * ─────────────────
 * All selectors use `page.getByTestId(...)` which resolves to
 * `[data-testid="..."]`. Stable across Tailwind refactors and Romanian text.
 *
 * USAGE
 * ─────
 * import { ActivationPage } from '../page-objects/activation.page';
 *
 * test('user can activate account with valid token', async ({ page }) => {
 *   const activation = new ActivationPage(page);
 *   await activation.goto('valid-token-uuid');
 *   await activation.fillPassword('SecurePass123!');
 *   await activation.fillPasswordConfirm('SecurePass123!');
 *   await activation.acceptGdpr();
 *   await activation.submit();
 *   const msg = await activation.getSuccessMessage();
 *   expect(msg).toBeTruthy();
 * });
 */

import type { Locator, Page } from '@playwright/test';

/**
 * ActivationPage
 *
 * Encapsulates all interactions with the /activate/[token] route.
 * Covers token validation states, the activation form, and success confirmation.
 */
export class ActivationPage {
  /** The raw Playwright Page object. Stored so methods can act on it. */
  private readonly page: Page;

  // ── Locators ───────────────────────────────────────────────────────────────
  // Locators are lazy — they do not query the DOM until an action or assertion
  // is called on them.

  /**
   * Spinner shown while the activation token is being validated server-side.
   * Maps to: <div data-testid="activate-loading" ...>
   * NOTE: Only rendered during the async token check (v-if).
   */
  readonly loadingIndicator: Locator;

  /**
   * Error banner shown when the token is invalid, expired, or already used.
   * Maps to: <div data-testid="activate-error" ...>
   * NOTE: Only rendered when an error exists (v-if).
   */
  readonly errorBanner: Locator;

  /**
   * Block showing the confirmed identity of the user being activated.
   * Typically displays: full name, role (e.g. "Profesor"), and school name.
   * This reassures the user they are activating the correct account.
   * Maps to: <div data-testid="activate-identity" ...>
   */
  readonly identityConfirmation: Locator;

  /**
   * The new password input field.
   * Maps to: <input data-testid="activate-password" type="password" ...>
   */
  readonly passwordInput: Locator;

  /**
   * The password confirmation input field (must match passwordInput).
   * Maps to: <input data-testid="activate-password-confirm" type="password" ...>
   */
  readonly passwordConfirmInput: Locator;

  /**
   * GDPR consent checkbox — only shown to parent accounts.
   * Per ROFUIP requirements, parents must explicitly accept the data
   * processing agreement before their child's data becomes visible.
   * Maps to: <input data-testid="activate-gdpr" type="checkbox" ...>
   */
  readonly gdprCheckbox: Locator;

  /**
   * The submit button that triggers account activation.
   * Maps to: <button data-testid="activate-submit" type="submit" ...>
   */
  readonly submitButton: Locator;

  /**
   * Success confirmation message shown after a successful activation.
   * Typically includes a prompt to log in.
   * Maps to: <div data-testid="activate-success" ...>
   * NOTE: Only rendered after successful form submission (v-if).
   */
  readonly successMessage: Locator;

  // ── Constructor ────────────────────────────────────────────────────────────
  /**
   * @param page - The Playwright Page instance injected by the test or fixture.
   */
  constructor(page: Page) {
    this.page = page;

    this.loadingIndicator = page.getByTestId('activate-loading');
    this.errorBanner = page.getByTestId('activate-error');
    this.identityConfirmation = page.getByTestId('activate-identity');
    this.passwordInput = page.getByTestId('activate-password');
    this.passwordConfirmInput = page.getByTestId('activate-password-confirm');
    this.gdprCheckbox = page.getByTestId('activate-gdpr');
    this.submitButton = page.getByTestId('activate-submit');
    this.successMessage = page.getByTestId('activate-success');
  }

  // ── Navigation ─────────────────────────────────────────────────────────────
  /**
   * goto
   *
   * Navigates to the activation page for the given token.
   * baseURL is defined in playwright.config.ts.
   *
   * @param token - The activation token from the email/SMS link,
   *                e.g. 'a1b2c3d4-e5f6-...' or a signed JWT string.
   */
  async goto(token: string): Promise<void> {
    await this.page.goto(`/activate/${token}`);
  }

  // ── State queries ──────────────────────────────────────────────────────────
  /**
   * isLoading
   *
   * Returns true while the page is still validating the activation token.
   *
   * @returns true if the loading indicator is visible, false otherwise.
   */
  async isLoading(): Promise<boolean> {
    return this.loadingIndicator.isVisible();
  }

  /**
   * getError
   *
   * Returns the trimmed text of the error banner, or null if no error
   * is currently displayed (token is valid, or loading is still in progress).
   *
   * @returns Error message string, or null.
   */
  async getError(): Promise<string | null> {
    const isVisible = await this.errorBanner.isVisible();
    if (!isVisible) {
      return null;
    }
    const text = await this.errorBanner.textContent();
    return text !== null ? text.trim() : null;
  }

  /**
   * getUserInfo
   *
   * Returns the trimmed text of the identity confirmation block, or null
   * if the block is not visible (still loading, or error state).
   *
   * @returns Identity text, e.g. "Ion Popescu — Profesor — Școala nr. 5", or null.
   */
  async getUserInfo(): Promise<string | null> {
    const isVisible = await this.identityConfirmation.isVisible();
    if (!isVisible) {
      return null;
    }
    const text = await this.identityConfirmation.textContent();
    return text !== null ? text.trim() : null;
  }

  /**
   * getSuccessMessage
   *
   * Returns the trimmed text of the success confirmation message, or null
   * if the success element is not yet visible (form not yet submitted, or
   * submission failed with a validation error).
   *
   * @returns Success message string, or null.
   */
  async getSuccessMessage(): Promise<string | null> {
    const isVisible = await this.successMessage.isVisible();
    if (!isVisible) {
      return null;
    }
    const text = await this.successMessage.textContent();
    return text !== null ? text.trim() : null;
  }

  // ── Form interactions ──────────────────────────────────────────────────────
  /**
   * fillPassword
   *
   * Types the chosen password into the password input field.
   * Clears any previously entered value first.
   *
   * @param pw - The desired password string. Must meet the app's password
   *             policy (min 8 chars, mixed case, number, symbol).
   */
  async fillPassword(pw: string): Promise<void> {
    // fill() replaces the entire field contents with the given string.
    await this.passwordInput.fill(pw);
  }

  /**
   * fillPasswordConfirm
   *
   * Types the password confirmation into the confirm field.
   * Must be identical to the value passed to fillPassword() for the form
   * to pass client-side validation.
   *
   * @param pw - Same password string used in fillPassword().
   */
  async fillPasswordConfirm(pw: string): Promise<void> {
    await this.passwordConfirmInput.fill(pw);
  }

  /**
   * acceptGdpr
   *
   * Checks the GDPR consent checkbox.
   * Only call this for parent-role activations where the checkbox is rendered.
   * For other roles the checkbox is not present and this call will throw.
   */
  async acceptGdpr(): Promise<void> {
    // check() is idempotent — if already checked it is a no-op.
    await this.gdprCheckbox.check();
  }

  /**
   * submit
   *
   * Clicks the activation submit button.
   * After this call the page will either:
   *   - Show the success message and optionally redirect to /login
   *   - Show a validation error inline (e.g. passwords don't match)
   */
  async submit(): Promise<void> {
    await this.submitButton.click();
  }
}
