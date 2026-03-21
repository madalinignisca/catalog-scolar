/**
 * Mock helpers for the native `fetch()` function used by `lib/api.ts`.
 *
 * IMPORTANT: `lib/api.ts` uses the browser-native `fetch()` directly — it does
 * NOT use Nuxt's `$fetch` / `useFetch`. Therefore we mock the global `fetch`
 * rather than `$fetch`.
 *
 * USAGE IN A TEST FILE
 * ────────────────────
 *
 *   import { createMockFetch, mockSuccessResponse, mockApiError } from
 *     '../helpers/mock-api';
 *
 *   // Install the mock before running the test:
 *   vi.stubGlobal('fetch', createMockFetch({
 *     '/auth/login': {
 *       status: 200,
 *       body: mockSuccessResponse({ access_token: 'abc', refresh_token: 'xyz' }),
 *     },
 *     '/auth/login-bad': {
 *       status: 401,
 *       body: mockApiError(401, 'INVALID_CREDENTIALS', 'Wrong password'),
 *     },
 *   }));
 *
 *   // vi.clearAllMocks() in setup.ts will clear call history between tests.
 *   // To remove the stub entirely: vi.unstubAllGlobals()
 */

// ── Types ────────────────────────────────────────────────────────────────────

/**
 * One entry in the route map passed to `createMockFetch`.
 *
 * `status`  — HTTP status code to return (e.g. 200, 401, 404).
 * `body`    — The response body. Can be any object; it will be JSON-serialised.
 * `headers` — Optional extra response headers (Content-Type is set
 *              automatically).
 */
export interface MockRoute {
  status: number;
  body: unknown;
  headers?: Record<string, string>;
}

/**
 * The shape of the error body our API server returns on failure.
 * Matches the `ApiErrorBody` interface in `lib/api.ts`.
 *
 * Example:
 *   { "error": { "code": "NOT_FOUND", "message": "Student not found" } }
 */
export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
  };
}

/**
 * A generic success-response envelope.
 * The actual API may return the data at the top level or nested — this helper
 * just wraps whatever you pass in, matching the way api<T>() parses the body:
 * `return response.json() as Promise<T>`.
 *
 * If your endpoint returns `{ students: [...] }`, pass that whole object as
 * `data` so the shape matches what the composable expects.
 */
export interface SuccessResponseEnvelope<T> {
  data: T;
}

// ── Helpers ──────────────────────────────────────────────────────────────────

/**
 * Build an error body that matches the `ApiError` structure thrown by
 * `lib/api.ts` when a response is not OK.
 *
 * @param status  - HTTP status code (stored on the thrown ApiError, not the
 *                  body itself, but documented here for caller convenience).
 * @param code    - Machine-readable error code, e.g. 'INVALID_CREDENTIALS'.
 * @param message - Human-readable message. Defaults to a generic string.
 *
 * @returns An `ApiErrorBody` object ready to pass as `body` in a `MockRoute`.
 *
 * Example:
 *   mockApiError(404, 'NOT_FOUND', 'Class not found')
 *   // → { error: { code: 'NOT_FOUND', message: 'Class not found' } }
 */
export function mockApiError(
  _status: number,
  code: string,
  message: string = 'An error occurred',
): ApiErrorBody {
  // `_status` is accepted as a parameter for documentation / readability at the
  // call site (so you can write `mockApiError(404, 'NOT_FOUND', ...)`) but the
  // body itself does not embed the HTTP status — that is carried by the
  // Response object's `.status` field, which is set in MockRoute.status.
  return {
    error: {
      code,
      message,
    },
  };
}

/**
 * Wrap a successful response payload in the standard envelope shape.
 *
 * Most CatalogRO API endpoints return data at the top level (not nested under
 * a `data` key), so in many cases you can just pass the raw object directly as
 * `body` in `MockRoute` without using this helper. Use this wrapper when your
 * test code reads `response.data`.
 *
 * @param data - The actual payload to wrap.
 * @returns    `{ data }` — a plain envelope object.
 *
 * @typeParam T - The TypeScript type of the payload (inferred automatically).
 *
 * Example:
 *   mockSuccessResponse({ id: '123', name: 'Ion Popescu' })
 *   // → { data: { id: '123', name: 'Ion Popescu' } }
 */
export function mockSuccessResponse<T>(data: T): SuccessResponseEnvelope<T> {
  return { data };
}

// ── Core factory ─────────────────────────────────────────────────────────────

/**
 * Create a fetch-compatible function that intercepts requests and returns
 * pre-configured responses, without making any real network calls.
 *
 * HOW ROUTE MATCHING WORKS
 * ────────────────────────
 * Routes are matched by checking whether the requested URL **ends with** the
 * route key.  This keeps test code clean — you write '/auth/login' instead of
 * the full 'http://localhost:8080/api/v1/auth/login'.
 *
 * Routes are checked in insertion order; the first match wins.  If no route
 * matches, the mock throws an error so you notice immediately that the test is
 * making an unexpected request.
 *
 * @param routes - A record mapping URL suffixes to `MockRoute` descriptors.
 * @returns       A function with the same signature as `window.fetch`.
 *
 * Example:
 *   vi.stubGlobal('fetch', createMockFetch({
 *     '/grades': { status: 200, body: [{ id: '1', numericGrade: 9 }] },
 *     '/grades/1': { status: 404, body: mockApiError(404, 'NOT_FOUND') },
 *   }));
 */
export function createMockFetch(
  routes: Record<string, MockRoute>,
): typeof fetch {
  /**
   * This is the function that replaces `window.fetch` during tests.
   *
   * It has the same signature as the native fetch:
   *   fetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response>
   *
   * We only use `input` for URL matching; `init` (method, headers, body) is
   * accepted but ignored — tests that need to assert on what was sent to the
   * server should spy on the mock function itself via `vi.fn()`.
   */
  return function mockFetch(
    input: RequestInfo | URL,
    _init?: RequestInit,
  ): Promise<Response> {
    // Normalise `input` to a plain string URL regardless of whether the caller
    // passed a string, a URL object, or a Request object.
    const url: string =
      typeof input === 'string'
        ? input
        : input instanceof URL
          ? input.href
          : input.url; // Request object

    // Find the first route whose key is a suffix of the requested URL.
    const routeEntry = Object.entries(routes).find(([key]) =>
      url.endsWith(key),
    );

    // If nothing matched, fail loudly so the test author knows they need to
    // add a route (or that the code under test is making an unexpected call).
    if (routeEntry === undefined) {
      return Promise.reject(
        new Error(
          `[mock-api] No mock route found for URL: "${url}"\n` +
            `Registered routes: ${Object.keys(routes).join(', ')}\n` +
            `Add a matching entry to createMockFetch({...}) in your test.`,
        ),
      );
    }

    const [, route] = routeEntry;

    // Serialise the response body to JSON exactly as a real server would.
    const bodyJson = JSON.stringify(route.body);

    // Build a proper `Response` object so the code under test can call
    // `.json()`, `.ok`, `.status`, etc. just like with a real response.
    return Promise.resolve(
      new Response(bodyJson, {
        status: route.status,
        headers: {
          'Content-Type': 'application/json',
          // Merge any caller-supplied headers (e.g. custom X- headers).
          ...route.headers,
        },
      }),
    );
  };
}
