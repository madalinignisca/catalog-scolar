import { api, clearTokens } from '~/lib/api';

interface User {
  id: string;
  schoolId: string;
  role: 'admin' | 'secretary' | 'teacher' | 'parent' | 'student';
  email: string | null;
  firstName: string;
  lastName: string;
  totpEnabled: boolean;
}

// NOTE: The api() wrapper auto-converts snake_case API keys to camelCase,
// so these field names match the CONVERTED output (not the raw API response).
interface LoginResponse {
  accessToken: string;
  refreshToken: string;
  mfaRequired?: boolean;
  mfaToken?: string;
}

const user = ref<User | null>(null);
const isAuthenticated = computed(() => user.value !== null);
const isLoading = ref(false);

export function useAuth() {
  /**
   * Authenticate with email + password.
   *
   * With cookie-based auth, the Go API sets httpOnly cookies in the login
   * response. We don't need to manually store tokens — the browser will
   * include the cookies automatically on subsequent requests (via
   * credentials: 'include' in the api() wrapper).
   *
   * If the user has 2FA enabled, we return { mfaRequired: true, mfaToken }
   * so the caller can prompt for the TOTP code.
   */
  async function login(
    email: string,
    password: string,
  ): Promise<{ mfaRequired: boolean; mfaToken?: string }> {
    isLoading.value = true;
    try {
      const data = await api<LoginResponse>('/auth/login', {
        method: 'POST',
        body: { email, password },
        skipAuth: true,
      });

      if (data.mfaRequired === true && data.mfaToken !== undefined && data.mfaToken !== '') {
        return { mfaRequired: true, mfaToken: data.mfaToken };
      }

      // No need to call setTokens() — the API response already set httpOnly
      // cookies. Just fetch the profile to populate the user ref.
      await fetchProfile();
      return { mfaRequired: false };
    } finally {
      isLoading.value = false;
    }
  }

  /**
   * Complete 2FA login by submitting the MFA token + TOTP code.
   *
   * On success the API sets httpOnly cookies — no manual token storage needed.
   */
  async function verifyMfa(mfaToken: string, totpCode: string): Promise<void> {
    await api<LoginResponse>('/auth/2fa/login', {
      method: 'POST',
      body: { mfa_token: mfaToken, totp_code: totpCode },
      skipAuth: true,
    });

    // Cookies are set by the API response. Just fetch the profile.
    await fetchProfile();
  }

  /**
   * Log out by calling the API (which clears httpOnly cookies with MaxAge=-1)
   * and resetting the local user state.
   */
  async function logout(): Promise<void> {
    try {
      await api('/auth/logout', { method: 'POST' });
    } finally {
      // clearTokens() removes any leftover localStorage keys from
      // before the cookie migration (one-time cleanup).
      clearTokens();
      user.value = null;
    }
  }

  /**
   * Fetch the authenticated user's profile from GET /users/me.
   *
   * With cookie-based auth, we don't need to check for a token in localStorage
   * first — just call the API. The httpOnly cookie is sent automatically via
   * credentials: 'include'. If the cookie is missing or expired, the API
   * returns 401 and we set user to null (which triggers the login redirect).
   */
  async function fetchProfile(): Promise<void> {
    try {
      // api() auto-unwraps the { data: ... } envelope AND converts snake_case
      // keys to camelCase, so we get a User-shaped object directly.
      user.value = await api<User>('/users/me');
    } catch {
      user.value = null;
    }
  }

  function requireRole(...roles: User['role'][]): boolean {
    return user.value !== null && roles.includes(user.value.role);
  }

  return {
    user: readonly(user),
    isAuthenticated,
    isLoading: readonly(isLoading),
    login,
    verifyMfa,
    logout,
    fetchProfile,
    requireRole,
  };
}
