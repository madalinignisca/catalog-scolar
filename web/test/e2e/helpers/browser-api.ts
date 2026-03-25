/**
 * browser-api.ts
 *
 * Helper for making authenticated API calls from within Playwright tests.
 *
 * BACKGROUND
 * ──────────
 * With cookie-based auth (#35), JWT tokens are stored in httpOnly cookies
 * that JavaScript cannot read. E2E tests that need to call the API directly
 * (e.g., to provision test data or verify server-side state) can no longer
 * extract the token from localStorage and pass it as an Authorization header.
 *
 * Instead, this helper runs fetch() inside the browser context (via
 * page.evaluate), where the httpOnly cookies are automatically included
 * when credentials: 'include' is set. The response is returned to the
 * Node.js test runner as a plain JSON object.
 *
 * USAGE
 * ─────
 * import { browserFetch, browserPost } from '../helpers/browser-api';
 *
 * // GET request
 * const users = await browserFetch(page, '/users');
 *
 * // POST request with body
 * const newUser = await browserPost(page, '/users', {
 *   email: 'test@school.ro',
 *   first_name: 'Test',
 *   last_name: 'User',
 *   role: 'parent',
 * });
 */

import type { Page } from '@playwright/test';

/** The API base URL — must match the Go server's listen address. */
const API_BASE = 'http://localhost:8080/api/v1';

/**
 * Response shape returned by browserFetch / browserPost.
 * Contains the HTTP status, whether it was successful, and the parsed JSON body.
 */
export interface BrowserFetchResult<T = unknown> {
  ok: boolean;
  status: number;
  data: T;
}

/**
 * Makes an authenticated GET request to the API from within the browser.
 *
 * The browser's httpOnly auth cookies are included automatically via
 * credentials: 'include'. No need to manually extract or pass tokens.
 *
 * @param page - Playwright Page with an active authenticated session.
 * @param path - API path (e.g., '/users' or '/classes/abc-123').
 * @returns The parsed JSON response body.
 */
export async function browserFetch<T = unknown>(
  page: Page,
  path: string,
): Promise<BrowserFetchResult<T>> {
  return page.evaluate(
    async ({ apiBase, apiPath }) => {
      const res = await fetch(`${apiBase}${apiPath}`, {
        method: 'GET',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
      });
      // eslint-disable-next-line @typescript-eslint/no-unsafe-assignment -- page.evaluate returns untyped JSON
      const json = await res.json().catch(() => null);
      return { ok: res.ok, status: res.status, data: json as never };
    },
    { apiBase: API_BASE, apiPath: path },
  );
}

/**
 * Makes an authenticated POST request to the API from within the browser.
 *
 * The browser's httpOnly auth cookies are included automatically via
 * credentials: 'include'. No need to manually extract or pass tokens.
 *
 * @param page - Playwright Page with an active authenticated session.
 * @param path - API path (e.g., '/users' or '/auth/2fa/setup').
 * @param body - The JSON body to send with the request.
 * @returns The parsed JSON response body.
 */
export async function browserPost<T = unknown>(
  page: Page,
  path: string,
  body?: Record<string, unknown>,
): Promise<BrowserFetchResult<T>> {
  return page.evaluate(
    async ({ apiBase, apiPath, apiBody }) => {
      const res = await fetch(`${apiBase}${apiPath}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: apiBody !== undefined ? JSON.stringify(apiBody) : undefined,
        credentials: 'include',
      });
      // eslint-disable-next-line @typescript-eslint/no-unsafe-assignment -- page.evaluate returns untyped JSON
      const json = await res.json().catch(() => null);
      return { ok: res.ok, status: res.status, data: json as never };
    },
    { apiBase: API_BASE, apiPath: path, apiBody: body },
  );
}

/**
 * Makes an authenticated PUT request to the API from within the browser.
 *
 * @param page - Playwright Page with an active authenticated session.
 * @param path - API path (e.g., '/users/abc-123').
 * @param body - The JSON body to send with the request.
 * @returns The parsed JSON response body.
 */
export async function browserPut<T = unknown>(
  page: Page,
  path: string,
  body?: Record<string, unknown>,
): Promise<BrowserFetchResult<T>> {
  return page.evaluate(
    async ({ apiBase, apiPath, apiBody }) => {
      const res = await fetch(`${apiBase}${apiPath}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: apiBody !== undefined ? JSON.stringify(apiBody) : undefined,
        credentials: 'include',
      });
      // eslint-disable-next-line @typescript-eslint/no-unsafe-assignment -- page.evaluate returns untyped JSON
      const json = await res.json().catch(() => null);
      return { ok: res.ok, status: res.status, data: json as never };
    },
    { apiBase: API_BASE, apiPath: path, apiBody: body },
  );
}

/**
 * Makes an authenticated DELETE request to the API from within the browser.
 *
 * @param page - Playwright Page with an active authenticated session.
 * @param path - API path (e.g., '/users/abc-123').
 * @returns The parsed JSON response body.
 */
export async function browserDelete<T = unknown>(
  page: Page,
  path: string,
): Promise<BrowserFetchResult<T>> {
  return page.evaluate(
    async ({ apiBase, apiPath }) => {
      const res = await fetch(`${apiBase}${apiPath}`, {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
      });
      // eslint-disable-next-line @typescript-eslint/no-unsafe-assignment -- page.evaluate returns untyped JSON
      const json = await res.json().catch(() => null);
      return { ok: res.ok, status: res.status, data: json as never };
    },
    { apiBase: API_BASE, apiPath: path },
  );
}
