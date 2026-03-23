/**
 * totp.ts
 *
 * Generates valid TOTP codes for E2E authentication tests.
 *
 * WHY THIS EXISTS
 * ───────────────
 * Admin, secretary, and teacher roles require 2FA (TOTP) to log in.
 * The seed data stores a known base32 secret (JBSWY3DPEHPK3PXP) for all
 * MFA-enabled users. This helper generates valid 6-digit codes from that
 * secret at runtime, so auth fixtures can complete the MFA step.
 *
 * TOTP WINDOW RACE MITIGATION
 * ────────────────────────────
 * TOTP codes are valid for 30 seconds. If we generate a code at second 28
 * of the window, it may expire before the form is submitted. The Go API
 * accepts +/- 1 time step (90-second effective window), but to be safe we
 * check the remaining time. If fewer than 5 seconds remain in the current
 * window, we wait for the next window before generating.
 */

import { TOTP, Secret } from 'otpauth';

/**
 * The base32-encoded TOTP secret shared by all MFA-enabled test users.
 * This MUST match the value stored in api/db/seed.sql.
 */
export const TEST_TOTP_SECRET = 'JBSWY3DPEHPK3PXP';

/** TOTP time step in seconds (standard RFC 6238 value). */
const TOTP_PERIOD = 30;

/** Minimum seconds remaining in the current window before we generate. */
const MIN_REMAINING_SECONDS = 5;

/**
 * generateTOTP
 *
 * Returns a valid 6-digit TOTP code for the given secret.
 * If the current time window has fewer than MIN_REMAINING_SECONDS left,
 * waits until the next window to avoid race conditions.
 *
 * @param secret - Base32-encoded TOTP secret. Defaults to TEST_TOTP_SECRET.
 * @returns A 6-character numeric string (e.g., "482913").
 */
export async function generateTOTP(secret: string = TEST_TOTP_SECRET): Promise<string> {
  // Check how many seconds remain in the current 30-second window.
  // TOTP windows align to Unix epoch, so: remaining = period - (now % period)
  const now = Math.floor(Date.now() / 1000);
  const remaining = TOTP_PERIOD - (now % TOTP_PERIOD);

  // If we are too close to the window boundary, wait for the next window.
  // This prevents generating a code that expires before the API validates it.
  if (remaining < MIN_REMAINING_SECONDS) {
    const waitMs = remaining * 1000 + 500; // +500ms safety margin
    await new Promise((resolve) => setTimeout(resolve, waitMs));
  }

  // Create a TOTP instance matching the API's configuration:
  // - SHA1 algorithm (RFC 6238 default, matches pquerna/otp Go library)
  // - 6 digits
  // - 30-second period
  // NOTE: otpauth v9+ requires a Secret object, not a raw string.
  const totp = new TOTP({
    secret: Secret.fromBase32(secret),
    digits: 6,
    period: TOTP_PERIOD,
    algorithm: 'SHA1',
  });

  return totp.generate();
}
