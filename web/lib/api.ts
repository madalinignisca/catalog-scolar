/**
 * Typed fetch wrapper for the CatalogRO API.
 * Handles JWT auth, token refresh, and tenant headers.
 */

const TOKEN_KEY = 'catalogro_access_token';
const REFRESH_KEY = 'catalogro_refresh_token';

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
    const token = getAccessToken();
    if (token !== null && token !== '') {
      headers['Authorization'] = `Bearer ${token}`;
    }
  }

  const response = await fetch(url, {
    method: options.method ?? 'GET',
    headers,
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
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
  if (typeof body === 'object' && body !== null && 'data' in body) {
    return (body as Record<string, unknown>)['data'] as T;
  }
  return body as T;
}

async function tryRefreshToken(): Promise<boolean> {
  if (!import.meta.client) return false;
  const refreshToken = localStorage.getItem(REFRESH_KEY);
  if (refreshToken === null || refreshToken === '') return false;

  try {
    const base = getApiBase();
    const response = await fetch(`${base}/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken }),
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
