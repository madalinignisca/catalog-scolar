/**
 * Typed fetch wrapper for the CatalogRO API.
 * Handles JWT auth, token refresh, and tenant headers.
 */

const TOKEN_KEY = 'catalogro_access_token';
const REFRESH_KEY = 'catalogro_refresh_token';

function getApiBase(): string {
  if (import.meta.client) {
    // Client-side: use the Nitro proxy (same-origin /api/v1).
    // This avoids cross-origin cookie issues in dev mode.
    // The proxy is configured in nuxt.config.ts routeRules.
    // In production, Traefik routes /api/* directly — same result.
    return '/api/v1';
  }
  // Server-side (SSR): call the Go API directly since the Nitro proxy
  // is for browser requests only. Override via NUXT_PUBLIC_API_BASE env var.
  const envBase = (process as unknown as { env: Record<string, string | undefined> }).env[
    'NUXT_PUBLIC_API_BASE'
  ];
  return envBase !== undefined && envBase !== '' ? envBase : 'http://localhost:8080/api/v1';
}

export function getAccessToken(): string | null {
  if (!import.meta.client) return null;
  return localStorage.getItem(TOKEN_KEY);
}

export function setTokens(access: string, refresh: string): void {
  if (!import.meta.client) return;
  localStorage.setItem(TOKEN_KEY, access);
  localStorage.setItem(REFRESH_KEY, refresh);
}

export function clearTokens(): void {
  if (!import.meta.client) return;
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(REFRESH_KEY);
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

interface RefreshResponse {
  access_token: string;
  refresh_token: string;
}

export async function api<T>(path: string, options: ApiOptions = {}): Promise<T> {
  const base = getApiBase();
  const url = `${base}${path}`;

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...options.headers,
  };

  if (!options.skipAuth) {
    // Client-side: add Bearer token from localStorage as fallback.
    // The httpOnly cookie is sent automatically via credentials: 'include',
    // but the Authorization header is kept for non-browser clients.
    const token = getAccessToken();
    if (token !== null && token !== '') {
      headers['Authorization'] = `Bearer ${token}`;
    }

    // SSR: forward the cookie header from the incoming Nuxt request so that
    // httpOnly auth cookies reach the API. Without this, SSR requests have
    // no auth context (localStorage is unavailable server-side).
    if (!import.meta.client) {
      try {
        const reqHeaders = useRequestHeaders(['cookie']);
        if (reqHeaders.cookie !== undefined && reqHeaders.cookie !== '') {
          headers['Cookie'] = reqHeaders.cookie;
        }
      } catch {
        // useRequestHeaders only works inside a Nuxt request context.
        // Outside of that (e.g., build-time), silently skip.
      }
    }
  }

  const response = await fetch(url, {
    method: options.method ?? 'GET',
    headers,
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    // Send httpOnly cookies with every request. The API sets auth cookies
    // alongside JSON tokens, enabling SSR auth without localStorage.
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

async function tryRefreshToken(): Promise<boolean> {
  const reqHeaders: Record<string, string> = { 'Content-Type': 'application/json' };
  let refreshToken: string | null = null;

  if (import.meta.client) {
    // Client-side: use refresh token from localStorage.
    refreshToken = localStorage.getItem(REFRESH_KEY);
    if (refreshToken === null || refreshToken === '') {
      return false;
    }
  } else {
    // SSR: forward cookies from the incoming request. The API will use the
    // httpOnly refresh_token cookie.
    try {
      const incoming = useRequestHeaders(['cookie']);
      if (incoming.cookie !== undefined && incoming.cookie !== '') {
        reqHeaders['Cookie'] = incoming.cookie;
      } else {
        return false;
      }
    } catch {
      return false;
    }
  }

  try {
    const base = getApiBase();
    const response = await fetch(`${base}/auth/refresh`, {
      method: 'POST',
      headers: reqHeaders,
      body:
        refreshToken !== null && refreshToken !== ''
          ? JSON.stringify({ refresh_token: refreshToken })
          : '{}',
      credentials: 'include',
    });

    if (!response.ok) return false;

    // Unwrap the { data: ... } envelope, same as the main api() function
    const body: unknown = await response.json();
    const inner =
      typeof body === 'object' && body !== null && 'data' in body
        ? (body as Record<string, unknown>)['data']
        : body;
    const data = inner as RefreshResponse;
    setTokens(data.access_token, data.refresh_token);
    return true;
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
