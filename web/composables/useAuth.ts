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

interface LoginResponse {
  access_token: string;
  refresh_token: string;
  mfa_required?: boolean;
  mfa_token?: string;
}

const user = ref<User | null>(null);
const isAuthenticated = computed(() => !!user.value);
const isLoading = ref(false);

export function useAuth() {
  async function login(email: string, password: string): Promise<{ mfaRequired: boolean; mfaToken?: string }> {
    isLoading.value = true;
    try {
      const data = await api<LoginResponse>('/auth/login', {
        method: 'POST',
        body: { email, password },
        skipAuth: true,
      });

      if (data.mfa_required && data.mfa_token) {
        return { mfaRequired: true, mfaToken: data.mfa_token };
      }

      setTokens(data.access_token, data.refresh_token);
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

    setTokens(data.access_token, data.refresh_token);
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
    if (!getAccessToken()) return;
    try {
      const data = await api<{ data: User }>('/users/me');
      user.value = data.data;
    } catch {
      user.value = null;
    }
  }

  function requireRole(...roles: User['role'][]): boolean {
    return !!user.value && roles.includes(user.value.role);
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
