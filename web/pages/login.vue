<script setup lang="ts">
definePageMeta({ layout: false });

const { login, verifyMfa } = useAuth();

const email = ref('');
const password = ref('');
const totpCode = ref('');
const error = ref('');
const loading = ref(false);
const mfaRequired = ref(false);
const mfaToken = ref('');

async function handleLogin() {
  error.value = '';
  loading.value = true;

  try {
    const result = await login(email.value, password.value);
    if (result.mfaRequired && result.mfaToken) {
      mfaRequired.value = true;
      mfaToken.value = result.mfaToken;
    } else {
      navigateTo('/');
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Autentificare eșuată';
  } finally {
    loading.value = false;
  }
}

async function handleMfa() {
  error.value = '';
  loading.value = true;

  try {
    await verifyMfa(mfaToken.value, totpCode.value);
    navigateTo('/');
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Cod invalid';
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center bg-gray-50 px-4">
    <div class="w-full max-w-md rounded-xl bg-white p-8 shadow-lg">
      <div class="mb-8 text-center">
        <h1 class="text-2xl font-bold text-gray-900">CatalogRO</h1>
        <p class="mt-1 text-sm text-gray-500">Catalog Școlar Digital</p>
      </div>

      <!-- Login form -->
      <form v-if="!mfaRequired" @submit.prevent="handleLogin" class="space-y-4">
        <div>
          <label for="email" class="block text-sm font-medium text-gray-700">Email</label>
          <input
            id="email"
            v-model="email"
            type="email"
            required
            autocomplete="email"
            class="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
          />
        </div>
        <div>
          <label for="password" class="block text-sm font-medium text-gray-700">Parolă</label>
          <input
            id="password"
            v-model="password"
            type="password"
            required
            autocomplete="current-password"
            class="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
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
          {{ loading ? 'Se autentifică...' : 'Autentificare' }}
        </button>
      </form>

      <!-- MFA form -->
      <form v-else @submit.prevent="handleMfa" class="space-y-4">
        <p class="text-sm text-gray-600">
          Introduceți codul din aplicația de autentificare (Google Authenticator, Authy etc.)
        </p>
        <div>
          <label for="totp" class="block text-sm font-medium text-gray-700">Cod 2FA</label>
          <input
            id="totp"
            v-model="totpCode"
            type="text"
            inputmode="numeric"
            pattern="[0-9]{6}"
            maxlength="6"
            required
            autocomplete="one-time-code"
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
          {{ loading ? 'Se verifică...' : 'Verificare' }}
        </button>
        <button
          type="button"
          @click="mfaRequired = false; mfaToken = ''"
          class="w-full text-sm text-gray-500 hover:text-gray-700"
        >
          ← Înapoi la autentificare
        </button>
      </form>
    </div>
  </div>
</template>
