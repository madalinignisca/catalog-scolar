<script setup lang="ts">
definePageMeta({ layout: false });

const route = useRoute();
const token = route.params['token'] as string;

const state = ref<'loading' | 'ready' | 'setting_password' | 'setting_2fa' | 'done' | 'error'>(
  'loading',
);
const error = ref('');

// Pre-populated data from server
const userData = ref<{
  schoolName: string;
  role: string;
  firstName: string;
  lastName: string;
  email: string | null;
  requires2fa: boolean;
  requiresGdpr: boolean;
} | null>(null);

// Form
const password = ref('');
const passwordConfirm = ref('');
const gdprConsent = ref(false);

// 2FA
const totpSecret = ref('');
const totpQr = ref('');
const totpCode = ref('');

const loading = ref(false);

onMounted(async () => {
  try {
    const data = await $fetch<{ data: typeof userData.value }>(`/api/v1/auth/activate/${token}`);
    userData.value = data.data;
    state.value = 'ready';
  } catch {
    state.value = 'error';
    error.value = 'Link de activare invalid sau expirat. Contactați secretariatul școlii.';
  }
});

async function handleActivate() {
  if (password.value !== passwordConfirm.value) {
    error.value = 'Parolele nu coincid';
    return;
  }
  if (password.value.length < 8) {
    error.value = 'Parola trebuie să aibă minim 8 caractere';
    return;
  }
  if (userData.value?.requiresGdpr === true && !gdprConsent.value) {
    error.value = 'Trebuie să acceptați termenii GDPR pentru a continua';
    return;
  }

  error.value = '';
  loading.value = true;
  state.value = 'setting_password';

  try {
    const body: Record<string, unknown> = {
      token,
      password: password.value,
    };
    if (userData.value?.requiresGdpr === true) {
      body['gdpr_consent'] = true;
    }

    const response = await $fetch<{
      access_token?: string;
      refresh_token?: string;
      mfa_setup_required?: boolean;
    }>('/api/v1/auth/activate', {
      method: 'POST',
      body,
    });

    if (response.mfa_setup_required === true) {
      // Fetch TOTP setup
      const setup = await $fetch<{ data: { secret: string; qr_code: string } }>(
        '/api/v1/auth/2fa/setup',
        {
          method: 'POST',
          headers: { Authorization: `Bearer ${response.access_token ?? ''}` },
        },
      );
      totpSecret.value = setup.data.secret;
      totpQr.value = setup.data.qr_code;
      state.value = 'setting_2fa';
    } else if (
      response.access_token !== undefined &&
      response.access_token !== '' &&
      response.refresh_token !== undefined &&
      response.refresh_token !== ''
    ) {
      // Direct login (parents/students)
      const { setTokens } = await import('~/lib/api');
      setTokens(response.access_token, response.refresh_token);
      state.value = 'done';
      setTimeout(() => {
        void navigateTo('/');
      }, 2000);
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Eroare la activare';
    state.value = 'ready';
  } finally {
    loading.value = false;
  }
}

async function handleVerify2fa() {
  loading.value = true;
  error.value = '';

  try {
    const response = await $fetch<{ access_token: string; refresh_token: string }>(
      '/api/v1/auth/2fa/verify',
      {
        method: 'POST',
        body: { totp_code: totpCode.value },
      },
    );

    const { setTokens } = await import('~/lib/api');
    setTokens(response.access_token, response.refresh_token);
    state.value = 'done';
    setTimeout(() => {
      void navigateTo('/');
    }, 2000);
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Cod invalid';
  } finally {
    loading.value = false;
  }
}

const roleLabels: Record<string, string> = {
  admin: 'Administrator',
  secretary: 'Secretariat',
  teacher: 'Profesor',
  parent: 'Părinte',
  student: 'Elev',
};
</script>

<template>
  <div class="flex min-h-screen items-center justify-center bg-gray-50 px-4">
    <div class="w-full max-w-lg rounded-xl bg-white p-8 shadow-lg">
      <div class="mb-6 text-center">
        <h1 class="text-2xl font-bold text-gray-900">CatalogRO</h1>
        <p class="mt-1 text-sm text-gray-500">Activare cont</p>
      </div>

      <!-- Loading -->
      <div v-if="state === 'loading'" class="py-8 text-center text-gray-500">
        Se verifică link-ul de activare...
      </div>

      <!-- Error -->
      <div v-else-if="state === 'error'" class="py-8 text-center">
        <p class="text-red-600">
          {{ error }}
        </p>
        <NuxtLink to="/login" class="mt-4 inline-block text-sm text-blue-600 hover:underline">
          Înapoi la autentificare
        </NuxtLink>
      </div>

      <!-- Ready: show user data + set password -->
      <div v-else-if="state === 'ready' && userData">
        <!-- Identity confirmation -->
        <div class="mb-6 rounded-lg bg-blue-50 p-4">
          <p class="text-sm font-medium text-blue-900">Confirmați identitatea</p>
          <div class="mt-2 space-y-1 text-sm text-blue-800">
            <p>
              <span class="font-medium">Nume:</span> {{ userData.firstName }}
              {{ userData.lastName }}
            </p>
            <p>
              <span class="font-medium">Rol:</span> {{ roleLabels[userData.role] ?? userData.role }}
            </p>
            <p><span class="font-medium">Școala:</span> {{ userData.schoolName }}</p>
          </div>
        </div>

        <form class="space-y-4" @submit.prevent="handleActivate">
          <div>
            <label for="activate-password" class="block text-sm font-medium text-gray-700"
              >Parolă nouă</label
            >
            <input
              id="activate-password"
              v-model="password"
              type="password"
              required
              minlength="8"
              autocomplete="new-password"
              class="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>
          <div>
            <label for="activate-password-confirm" class="block text-sm font-medium text-gray-700"
              >Confirmă parola</label
            >
            <input
              id="activate-password-confirm"
              v-model="passwordConfirm"
              type="password"
              required
              minlength="8"
              autocomplete="new-password"
              class="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>

          <!-- GDPR consent for parents -->
          <div v-if="userData.requiresGdpr" class="rounded-lg border border-gray-200 p-4">
            <label class="flex items-start gap-3">
              <input
                v-model="gdprConsent"
                type="checkbox"
                class="mt-0.5 h-4 w-4 rounded border-gray-300 text-blue-600"
              />
              <span class="text-sm text-gray-700">
                Accept prelucrarea datelor personale ale copilului meu conform
                <a href="#" class="text-blue-600 hover:underline">Politicii de Confidențialitate</a>
                și Regulamentului GDPR (UE 2016/679).
              </span>
            </label>
          </div>

          <div v-if="error" class="rounded-lg bg-red-50 p-3 text-sm text-red-700">
            {{ error }}
          </div>

          <button
            type="submit"
            :disabled="loading"
            class="w-full rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-blue-700 disabled:opacity-50"
          >
            {{ loading ? 'Se activează...' : 'Activare cont' }}
          </button>
        </form>
      </div>

      <!-- 2FA setup -->
      <div v-else-if="state === 'setting_2fa'">
        <div class="mb-4 text-center">
          <p class="text-sm text-gray-600">
            Contul dumneavoastră necesită autentificare cu doi factori (2FA). Scanați codul QR cu
            aplicația de autentificare.
          </p>
        </div>

        <div v-if="totpQr" class="mb-4 flex justify-center">
          <img :src="totpQr" alt="QR Code TOTP" class="h-48 w-48" />
        </div>

        <p class="mb-4 text-center text-xs text-gray-400">
          Sau introduceți manual: <code class="text-gray-600">{{ totpSecret }}</code>
        </p>

        <form class="space-y-4" @submit.prevent="handleVerify2fa">
          <div>
            <label for="totp-verify" class="block text-sm font-medium text-gray-700"
              >Cod verificare</label
            >
            <input
              id="totp-verify"
              v-model="totpCode"
              type="text"
              inputmode="numeric"
              pattern="[0-9]{6}"
              maxlength="6"
              required
              class="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-center text-2xl tracking-widest shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>
          <div v-if="error" class="rounded-lg bg-red-50 p-3 text-sm text-red-700">
            {{ error }}
          </div>
          <button
            type="submit"
            :disabled="loading"
            class="w-full rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-blue-700 disabled:opacity-50"
          >
            {{ loading ? 'Se verifică...' : 'Finalizare activare' }}
          </button>
        </form>
      </div>

      <!-- Done -->
      <div v-else-if="state === 'done'" class="py-8 text-center">
        <div
          class="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-green-100"
        >
          <svg class="h-6 w-6 text-green-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M5 13l4 4L19 7"
            />
          </svg>
        </div>
        <p class="text-lg font-semibold text-gray-900">Cont activat cu succes!</p>
        <p class="mt-1 text-sm text-gray-500">Redirecționare...</p>
      </div>
    </div>
  </div>
</template>
