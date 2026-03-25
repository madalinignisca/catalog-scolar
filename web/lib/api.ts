/**
 * Typed fetch wrapper for the CatalogRO API.
 *
 * Authentication uses httpOnly cookies set by the Go API on login/refresh.
 * The browser sends these cookies automatically with every request via
 * `credentials: 'include'`. This works for both client-side navigation
 * and Nuxt SSR (where localStorage is unavailable).
 *
 * The old localStorage-based token storage (getAccessToken, setTokens,
 * clearTokens) is kept as no-ops for backward compatibility — callers
 * that still reference them won't break, but the functions do nothing.
 */

function getApiBase(): string {
  // Check for explicit override via env var first (works in both client and server).
  if (import.meta.client) {
    const configured = useRuntimeConfig().public.apiBase;
    // If the configured base is the default localhost and we're accessing the app
    // from a different host (e.g., VM IP on the LAN), use the current browser hostname
    // so the API call goes to the same machine the page was loaded from.
    if (configured === 'http://localhost:8080/api/v1' && window.location.hostname !== 'localhost') {
      return `http://${window.location.hostname}:8080/api/v1`;
    }
    return configured;
  }
  // Server-side: process.env is available via Node but not typed by @types/node in this project.
  const envBase = (process as unknown as { env: Record<string, string | undefined> }).env[
    'NUXT_PUBLIC_API_BASE'
  ];
  return envBase !== undefined && envBase !== '' ? envBase : 'http://localhost:8080/api/v1';
}

/**
 * No-op — kept for backward compatibility.
 * Auth tokens are now stored in httpOnly cookies, not localStorage.
 * Always returns null because the cookie is httpOnly (JS can't read it).
 */
export function getAccessToken(): string | null {
  return null;
}

/**
 * No-op — kept for backward compatibility.
 * Auth tokens are now set as httpOnly cookies by the Go API response.
 * Callers that still reference setTokens() (e.g., useAuth.login) won't
 * break — the function simply does nothing because the API already set
 * the cookies in the HTTP response headers.
 */
export function setTokens(_access: string, _refresh: string): void {
  // Intentionally empty — cookies are set by the API, not by JS.
}

/**
 * No-op — kept for backward compatibility.
 * Cookie clearing is handled server-side by the logout endpoint
 * (which sets MaxAge=-1 on both cookies).
 */
export function clearTokens(): void {
  // Intentionally empty — cookies are cleared by the logout API response.
  // Also clean up any leftover localStorage keys from before the migration.
  if (import.meta.client) {
    localStorage.removeItem('catalogro_access_token');
    localStorage.removeItem('catalogro_refresh_token');
  }
}

interface ApiOptions {
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
  skipAuth?: boolean;
}

interface ApiErrorBody {
  error?: {
    code?: string;
    message?: string;
  };
}

export async function api<T>(path: string, options: ApiOptions = {}): Promise<T> {
  const base = getApiBase();
  const url = `${base}${path}`;

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...options.headers,
  };

  // No Authorization header needed — httpOnly cookies are sent automatically
  // by the browser when credentials: 'include' is set on the fetch call.

  const response = await fetch(url, {
    method: options.method ?? 'GET',
    headers,
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    // credentials: 'include' tells the browser to send cookies with this
    // request, even for cross-origin calls (e.g., localhost:3000 → localhost:8080).
    // Without this, the httpOnly auth cookies would not be included.
    credentials: 'include',
  });

  // Token expired — try refresh
  if (response.status === 401 && !options.skipAuth) {
    const refreshed = await tryRefreshToken();
    if (refreshed) {
      return api<T>(path, options); // retry with new token
    }
    // Refresh failed — redirect to login
    if (import.meta.client) {
      clearTokens();
      window.location.href = '/login';
    }
  }

  if (!response.ok) {
    const errorBody = (await response
      .json()
      .catch(() => ({ error: { message: response.statusText } }))) as ApiErrorBody;
    throw new ApiError(
      response.status,
      errorBody.error?.code ?? 'UNKNOWN',
      errorBody.error?.message ?? 'Request failed',
    );
  }

  // The API wraps all success responses in { "data": ... }. Unwrap the envelope
  // so callers get the inner payload directly (e.g., api<User>('/users/me')
  // returns the User object, not { data: User }).
  const body: unknown = await response.json();
  let payload: unknown;
  if (typeof body === 'object' && body !== null && 'data' in body) {
    payload = (body as Record<string, unknown>)['data'];
  } else {
    payload = body;
  }

  // Convert snake_case keys from the Go API to camelCase used by TypeScript
  // interfaces throughout the frontend. This is done recursively so nested
  // objects (e.g., homeroom_teacher inside a class) are also converted.
  return snakeToCamel(payload) as T;
}

/**
 * Recursively converts snake_case object keys to camelCase.
 * Handles arrays, nested objects, and primitive values.
 *
 * Examples:
 *   { first_name: "Ana" }         → { firstName: "Ana" }
 *   { school_id: "abc" }          → { schoolId: "abc" }
 *   [{ grade_date: "2026-01-01" }] → [{ gradeDate: "2026-01-01" }]
 */
function snakeToCamel(data: unknown): unknown {
  if (Array.isArray(data)) {
    return data.map(snakeToCamel);
  }
  if (data !== null && typeof data === 'object' && !(data instanceof Date)) {
    const result: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(data as Record<string, unknown>)) {
      const camelKey = key.replace(/_([a-z])/g, (_, letter: string) => letter.toUpperCase());
      result[camelKey] = snakeToCamel(value);
    }
    return result;
  }
  return data;
}

/**
 * Attempts to refresh the access token by calling POST /auth/refresh.
 *
 * With cookie-based auth, the refresh token is stored in an httpOnly cookie
 * scoped to /api/v1/auth/refresh. The browser sends it automatically when
 * we call the refresh endpoint with credentials: 'include'. No need to
 * read the token from localStorage or pass it in the JSON body — the
 * cookie handles everything.
 *
 * On success, the API response sets new cookies (token rotation) and we
 * return true so the caller can retry the original request.
 */
async function tryRefreshToken(): Promise<boolean> {
  try {
    const base = getApiBase();
    const response = await fetch(`${base}/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      // Send an empty JSON body — the refresh token comes from the cookie.
      // The Go API's HandleRefresh reads from cookie first, JSON body second.
      body: JSON.stringify({}),
      // credentials: 'include' is critical here — it sends the refresh token
      // cookie (scoped to /api/v1/auth/refresh) with this request.
      credentials: 'include',
    });

    // If the refresh succeeded, the API response already set new cookies
    // (access + refresh). No need to manually store anything.
    return response.ok;
  } catch {
    return false;
  }
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}
