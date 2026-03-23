/**
 * grade-input.page.ts
 *
 * Page Object Model (POM) for the CatalogRO grade input modal.
 *
 * WHAT THIS COMPONENT DOES
 * ────────────────────────
 * The grade input modal is an overlay dialog that appears when a teacher
 * clicks "add grade" or an existing grade badge in the catalog view. It allows
 * entering or editing a single grade entry for one student. Depending on the
 * school's evaluation configuration the form shows either:
 *   - A numeric input (grades 1–10, middle school / high school)
 *   - Qualifier buttons: FB (Foarte Bine), B (Bine), S (Suficient), I (Insuficient)
 *     (primary school, classes P–IV)
 *
 * The modal can be dismissed by clicking the backdrop, the Cancel button,
 * or (on success) the Save button.
 *
 * SELECTOR STRATEGY
 * ─────────────────
 * All selectors use `page.getByTestId(...)` which resolves to
 * `[data-testid="..."]`. These are stable across Tailwind refactors and
 * Romanian text changes.
 *
 * USAGE
 * ─────
 * import { GradeInputModal } from '../page-objects/grade-input.page';
 *
 * test('teacher adds a numeric grade', async ({ page }) => {
 *   const modal = new GradeInputModal(page);
 *   // (assume catalog page is open and add-grade was clicked)
 *   await expect(modal.modal).toBeVisible();
 *   await modal.fillNumericGrade(9);
 *   await modal.setDate('2025-03-15');
 *   await modal.save();
 *   expect(await modal.isVisible()).toBe(false);
 * });
 */

import type { Locator, Page } from '@playwright/test';

/**
 * GradeInputModal
 *
 * Encapsulates all interactions with the grade input overlay modal.
 * Works for both numeric grade entry (middle/high school) and qualifier
 * selection (primary school).
 */
export class GradeInputModal {
  /** The raw Playwright Page object. Stored so methods can act on it. */
  private readonly page: Page;

  // ── Locators ───────────────────────────────────────────────────────────────
  // Locators are lazy — they do not query the DOM until an action or assertion
  // is called on them.

  /**
   * Semi-transparent backdrop behind the modal.
   * Clicking it dismisses the modal without saving.
   * Maps to: <div data-testid="grade-modal-backdrop" ...>
   */
  readonly backdrop: Locator;

  /**
   * The modal dialog container itself.
   * Use this to assert that the modal is visible before interacting with it.
   * Maps to: <div data-testid="grade-modal" role="dialog" ...>
   */
  readonly modal: Locator;

  /**
   * The modal title, e.g. "Adaugă notă" (add) or "Editează nota" (edit).
   * Maps to: <h2 data-testid="grade-modal-title" ...>
   */
  readonly title: Locator;

  /**
   * The student's full name displayed at the top of the modal.
   * Helps verify that the modal opened for the correct student.
   * Maps to: <p data-testid="grade-modal-student" ...>
   */
  readonly studentName: Locator;

  /**
   * Numeric grade input field (1–10), visible for middle/high school classes.
   * Maps to: <input data-testid="grade-numeric-input" type="number" ...>
   * NOTE: Only rendered when school's evaluation_config uses numeric grading.
   */
  readonly numericInput: Locator;

  /**
   * Date picker input for the date the grade was awarded.
   * Expects ISO format YYYY-MM-DD.
   * Maps to: <input data-testid="grade-date-input" type="date" ...>
   */
  readonly dateInput: Locator;

  /**
   * Optional free-text description / teacher note for the grade entry.
   * Maps to: <textarea data-testid="grade-description-input" ...>
   */
  readonly descriptionInput: Locator;

  /**
   * Inline validation error shown when the form is submitted with invalid data
   * (e.g. grade out of range, missing date).
   * Maps to: <p data-testid="grade-validation-error" ...>
   * NOTE: Only rendered when there is an active validation error (v-if).
   */
  readonly validationError: Locator;

  /**
   * Cancel button — closes the modal without saving any changes.
   * Maps to: <button data-testid="grade-cancel-button" ...>
   */
  readonly cancelButton: Locator;

  /**
   * Save button — submits the grade form.
   * On success the modal closes and the grade appears in the catalog grid.
   * On failure a validation error is shown.
   * Maps to: <button data-testid="grade-save-button" type="submit" ...>
   */
  readonly saveButton: Locator;

  // ── Constructor ────────────────────────────────────────────────────────────
  /**
   * @param page - The Playwright Page instance injected by the test or fixture.
   */
  constructor(page: Page) {
    this.page = page;

    this.backdrop = page.getByTestId('grade-modal-backdrop');
    this.modal = page.getByTestId('grade-modal');
    this.title = page.getByTestId('grade-modal-title');
    this.studentName = page.getByTestId('grade-modal-student');
    this.numericInput = page.getByTestId('grade-numeric-input');
    this.dateInput = page.getByTestId('grade-date-input');
    this.descriptionInput = page.getByTestId('grade-description-input');
    this.validationError = page.getByTestId('grade-validation-error');
    this.cancelButton = page.getByTestId('grade-cancel-button');
    this.saveButton = page.getByTestId('grade-save-button');
  }

  // ── State queries ──────────────────────────────────────────────────────────
  /**
   * isVisible
   *
   * Returns true when the modal dialog is currently visible on screen.
   * Use this to assert that the modal opened after clicking "add grade",
   * or that it closed after a successful save or cancel.
   *
   * @returns true if the modal is visible, false otherwise.
   */
  async isVisible(): Promise<boolean> {
    return this.modal.isVisible();
  }

  /**
   * getTitle
   *
   * Returns the trimmed text content of the modal title.
   *
   * @returns Modal title string, e.g. "Adaugă notă" or "Editează nota".
   */
  async getTitle(): Promise<string> {
    const text = await this.title.textContent();
    return text !== null ? text.trim() : '';
  }

  /**
   * getStudentName
   *
   * Returns the trimmed name of the student shown in the modal header.
   * Use this to verify the correct student's grade form is open.
   *
   * @returns Student full name string, e.g. "Ion Popescu".
   */
  async getStudentName(): Promise<string> {
    const text = await this.studentName.textContent();
    return text !== null ? text.trim() : '';
  }

  /**
   * getValidationError
   *
   * Returns the trimmed text of the inline validation error, or null if
   * no validation error is currently displayed.
   *
   * @returns Validation error string, or null.
   */
  async getValidationError(): Promise<string | null> {
    // isVisible() returns false when the element is absent from the DOM
    // (v-if removes it when there is no active error).
    const isVisible = await this.validationError.isVisible();
    if (!isVisible) {
      return null;
    }
    const text = await this.validationError.textContent();
    return text !== null ? text.trim() : null;
  }

  // ── Form interactions ──────────────────────────────────────────────────────
  /**
   * selectQualifier
   *
   * Clicks the qualifier button for the given qualifier value.
   * Only applicable for primary-school (P–IV) evaluation mode.
   * The qualifier buttons have testids: qualifier-FB, qualifier-B,
   * qualifier-S, qualifier-I.
   *
   * @param q - One of 'FB' (Foarte Bine), 'B' (Bine), 'S' (Suficient),
   *            or 'I' (Insuficient).
   */
  async selectQualifier(q: 'FB' | 'B' | 'S' | 'I'): Promise<void> {
    // Each qualifier has its own testid derived from the qualifier letter(s).
    await this.page.getByTestId(`qualifier-${q}`).click();
  }

  /**
   * fillNumericGrade
   *
   * Clears the numeric input and types the given grade value.
   * Valid range is 1–10 per Romanian grading rules.
   * Only applicable for middle-school / high-school evaluation mode.
   *
   * @param n - Integer grade between 1 and 10 inclusive.
   */
  async fillNumericGrade(n: number): Promise<void> {
    // fill() replaces any existing value in the field.
    await this.numericInput.fill(String(n));
  }

  /**
   * setDate
   *
   * Sets the date the grade was awarded.
   * Date must be in ISO 8601 format (YYYY-MM-DD), e.g. '2025-03-15'.
   *
   * @param date - ISO date string, e.g. '2025-03-15'.
   */
  async setDate(date: string): Promise<void> {
    await this.dateInput.fill(date);
  }

  /**
   * fillDescription
   *
   * Types an optional teacher note into the description textarea.
   * This text is stored alongside the grade for context.
   *
   * @param text - Free-form description, e.g. 'Lucrare de control capitol 3'.
   */
  async fillDescription(text: string): Promise<void> {
    await this.descriptionInput.fill(text);
  }

  /**
   * save
   *
   * Clicks the Save button to submit the grade form.
   * After this call the modal will either:
   *   - Close (success — grade was saved, catalog grid updates)
   *   - Stay open with a validation error visible (invalid input)
   */
  async save(): Promise<void> {
    await this.saveButton.click();
  }

  /**
   * cancel
   *
   * Clicks the Cancel button to close the modal without saving.
   * Any filled values are discarded.
   */
  async cancel(): Promise<void> {
    await this.cancelButton.click();
  }

  /**
   * close
   *
   * Clicks the modal backdrop to dismiss the modal without saving.
   * This simulates a user clicking outside the dialog — a common pattern
   * for accidental close-without-save scenarios.
   */
  async close(): Promise<void> {
    await this.backdrop.click();
  }
}
