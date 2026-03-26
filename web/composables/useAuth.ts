import { api, setTokens, clearTokens, getAccessToken } from '~/lib/api';

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

      setTokens(data.accessToken, data.refreshToken);
      await fetchProfile();
      return { mfaRequired: false };
    } finally {
      isLoading.value = false;
    }
  }

  async function verifyMfa(mfaToken: string, totpCode: string): Promise<void> {
    const data = await api<LoginResponse>('/auth/2fa/login', {
      method: 'POST',
      body: { mfa_token: mfaToken, totp_code: totpCode },
      skipAuth: true,
    });

    setTokens(data.accessToken, data.refreshToken);
    await fetchProfile();
  }

  async function logout(): Promise<void> {
    try {
      await api('/auth/logout', { method: 'POST' });
    } finally {
      clearTokens();
      user.value = null;
    }
  }

  async function fetchProfile(): Promise<void> {
    const token = getAccessToken();
    if (token === null || token === '') return;
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
