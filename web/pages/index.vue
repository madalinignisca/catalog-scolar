<script setup lang="ts">
const { user, isAuthenticated, logout } = useAuth();
const { isOnline, pendingMutations } = useOfflineSync();

// Redirect to login if not authenticated
if (import.meta.client && !isAuthenticated.value) {
  navigateTo('/login');
}
</script>

<template>
  <div class="min-h-screen bg-gray-50">
    <!-- Top bar -->
    <header class="border-b border-gray-200 bg-white shadow-sm">
      <div class="mx-auto flex max-w-7xl items-center justify-between px-4 py-3">
        <div class="flex items-center gap-3">
          <h1 class="text-lg font-bold text-gray-900">CatalogRO</h1>
          <!-- Sync status -->
          <span
            :class="[
              'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium',
              isOnline ? 'bg-green-100 text-green-700' : 'bg-yellow-100 text-yellow-700',
            ]"
          >
            <span
              :class="['h-1.5 w-1.5 rounded-full', isOnline ? 'bg-green-500' : 'bg-yellow-500']"
            />
            {{ isOnline ? 'Online' : 'Offline' }}
            <template v-if="pendingMutations > 0">
              · {{ pendingMutations }} nesincronizate
            </template>
          </span>
        </div>

        <div class="flex items-center gap-4">
          <span v-if="user" class="text-sm text-gray-600">
            {{ user.firstName }} {{ user.lastName }}
            <span class="ml-1 rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-500">
              {{ user.role }}
            </span>
          </span>
          <button
            @click="logout().then(() => navigateTo('/login'))"
            class="text-sm text-gray-500 hover:text-gray-700"
          >
            Ieșire
          </button>
        </div>
      </div>
    </header>

    <!-- Content -->
    <main class="mx-auto max-w-7xl px-4 py-8">
      <!-- Teacher dashboard -->
      <div v-if="user?.role === 'teacher'" class="space-y-6">
        <h2 class="text-xl font-semibold text-gray-900">Clasele mele</h2>
        <p class="text-gray-500">Încărcare clase...</p>
        <!-- TODO: fetch and display teacher's classes -->
      </div>

      <!-- Parent dashboard -->
      <div v-else-if="user?.role === 'parent'" class="space-y-6">
        <h2 class="text-xl font-semibold text-gray-900">Copiii mei</h2>
        <p class="text-gray-500">Încărcare date...</p>
        <!-- TODO: fetch and display children -->
      </div>

      <!-- Admin dashboard -->
      <div v-else-if="user?.role === 'admin'" class="space-y-6">
        <h2 class="text-xl font-semibold text-gray-900">Administrare școală</h2>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <NuxtLink
            to="/admin/users"
            class="rounded-xl border border-gray-200 bg-white p-6 shadow-sm hover:shadow-md"
          >
            <h3 class="font-semibold text-gray-900">Utilizatori</h3>
            <p class="mt-1 text-sm text-gray-500">Provizionare conturi, activări în așteptare</p>
          </NuxtLink>
          <NuxtLink
            to="/admin/classes"
            class="rounded-xl border border-gray-200 bg-white p-6 shadow-sm hover:shadow-md"
          >
            <h3 class="font-semibold text-gray-900">Clase & Materii</h3>
            <p class="mt-1 text-sm text-gray-500">Încadrare, formațiuni de studiu</p>
          </NuxtLink>
          <NuxtLink
            to="/reports"
            class="rounded-xl border border-gray-200 bg-white p-6 shadow-sm hover:shadow-md"
          >
            <h3 class="font-semibold text-gray-900">Rapoarte</h3>
            <p class="mt-1 text-sm text-gray-500">Dashboard, statistici, export ISJ</p>
          </NuxtLink>
        </div>
      </div>

      <!-- Fallback -->
      <div v-else class="text-center text-gray-500">
        <p>Bine ai venit în CatalogRO</p>
      </div>
    </main>
  </div>
</template>
