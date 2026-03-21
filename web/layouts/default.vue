<!--
  default.vue — Main application layout for authenticated pages.

  This layout wraps all pages that require authentication (dashboard,
  catalog, absences, etc.). It provides:
  - A top navigation bar with the CatalogRO logo, school name, user info,
    sync status indicator, and a logout button.
  - A collapsible sidebar with navigation links (Dashboard, Catalog, Absences).
  - A main content area where the page content renders via <slot />.
  - Mobile-responsive design using Tailwind CSS.

  Pages that do NOT use this layout (like /login) must set
  `definePageMeta({ layout: false })` in their <script setup>.
-->

<script setup lang="ts">
/**
 * Get the current user and auth state from the auth composable.
 * If the user is not logged in, we redirect them to /login.
 */
const { user, isAuthenticated, logout, fetchProfile } = useAuth();

/**
 * Get the current school info (tenant context).
 * We display the school name in the top bar.
 */
const { currentSchool, fetchCurrentSchool } = useTenant();

/**
 * Track whether the mobile sidebar is open.
 * On desktop, the sidebar is always visible.
 * On mobile, it slides in/out via this toggle.
 */
const isSidebarOpen = ref(false);

/**
 * On component mount (client-side only):
 * 1. Check if the user is authenticated; if not, redirect to login.
 * 2. If authenticated but profile not loaded, fetch it.
 * 3. Fetch the current school info for the top bar.
 */
onMounted(async () => {
  if (!isAuthenticated.value) {
    await navigateTo('/login');
    return;
  }

  /* If we have a token but no user profile loaded (e.g. page refresh),
   * fetch the profile from GET /users/me */
  if (user.value === null) {
    await fetchProfile();
  }

  /* Fetch the school info for display in the navigation bar */
  await fetchCurrentSchool();
});

/**
 * Handle the logout button click.
 * Calls the auth logout (revokes tokens) then redirects to /login.
 */
async function handleLogout(): Promise<void> {
  await logout();
  await navigateTo('/login');
}

/**
 * Toggle the mobile sidebar open/closed.
 */
function toggleSidebar(): void {
  isSidebarOpen.value = !isSidebarOpen.value;
}

/**
 * Close the mobile sidebar (e.g. after clicking a nav link on mobile).
 */
function closeSidebar(): void {
  isSidebarOpen.value = false;
}

/**
 * Navigation items displayed in the sidebar.
 * Each item has a label (in Romanian), a route path, and an SVG icon.
 */
const navItems = [
  {
    label: 'Tablou de bord',
    to: '/',
    /** Home/dashboard icon */
    icon: 'M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6',
  },
  {
    label: 'Catalog',
    to: '/',
    /** Book/catalog icon */
    icon: 'M12 6.253v13m0-13C10.832 5.477 9.246 5 7.5 5S4.168 5.477 3 6.253v13C4.168 18.477 5.754 18 7.5 18s3.332.477 4.5 1.253m0-13C13.168 5.477 14.754 5 16.5 5c1.747 0 3.332.477 4.5 1.253v13C19.832 18.477 18.247 18 16.5 18c-1.746 0-3.332.477-4.5 1.253',
  },
  {
    label: 'Absențe',
    to: '/absences',
    /** Calendar/absence icon */
    icon: 'M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z',
  },
];

/**
 * Map user roles to human-readable Romanian labels for display.
 */
const roleLabels: Record<string, string> = {
  admin: 'Administrator',
  secretary: 'Secretar',
  teacher: 'Profesor',
  parent: 'Părinte',
  student: 'Elev',
};
</script>

<template>
  <div class="flex min-h-screen bg-gray-50">
    <!-- ================================================================== -->
    <!-- MOBILE SIDEBAR OVERLAY                                             -->
    <!-- Shown only on small screens when the sidebar is toggled open.      -->
    <!-- Clicking the overlay closes the sidebar.                           -->
    <!-- ================================================================== -->
    <button
      v-if="isSidebarOpen"
      type="button"
      aria-label="Închide meniul"
      class="fixed inset-0 z-30 cursor-default border-none bg-black/50 lg:hidden"
      @click="closeSidebar"
    />

    <!-- ================================================================== -->
    <!-- SIDEBAR NAVIGATION                                                 -->
    <!-- On desktop (lg+): always visible, fixed width.                     -->
    <!-- On mobile (<lg): slides in from the left, overlays content.        -->
    <!-- ================================================================== -->
    <aside
      :class="[
        'fixed inset-y-0 left-0 z-40 flex w-64 flex-col border-r border-gray-200 bg-white transition-transform duration-200 ease-in-out lg:static lg:translate-x-0',
        isSidebarOpen ? 'translate-x-0' : '-translate-x-full',
      ]"
    >
      <!-- Sidebar header: school name -->
      <div class="flex h-16 items-center border-b border-gray-200 px-4">
        <div class="min-w-0 flex-1">
          <h2 class="truncate text-sm font-semibold text-gray-900">
            {{ currentSchool?.name ?? 'CatalogRO' }}
          </h2>
          <p class="truncate text-xs text-gray-500">
            Catalog Școlar Digital
          </p>
        </div>
        <!-- Close button (mobile only) -->
        <button
          class="ml-2 rounded-lg p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-600 lg:hidden"
          @click="closeSidebar"
        >
          <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </div>

      <!-- Navigation links -->
      <nav class="flex-1 space-y-1 px-3 py-4">
        <NuxtLink
          v-for="item in navItems"
          :key="item.label"
          :to="item.to"
          class="flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium text-gray-700 transition-colors hover:bg-blue-50 hover:text-blue-700"
          active-class="bg-blue-50 text-blue-700"
          @click="closeSidebar"
        >
          <!-- Icon for this nav item (SVG path rendered via the icon string) -->
          <svg
            class="h-5 w-5 flex-shrink-0"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="1.5"
          >
            <path stroke-linecap="round" stroke-linejoin="round" :d="item.icon" />
          </svg>
          {{ item.label }}
        </NuxtLink>
      </nav>

      <!-- Sidebar footer: user info (visible on desktop sidebar) -->
      <div class="border-t border-gray-200 p-4">
        <div v-if="user" class="flex items-center gap-3">
          <!-- User avatar placeholder (initials) -->
          <div
            class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-blue-100 text-xs font-semibold text-blue-700"
          >
            {{ user.firstName.charAt(0) }}{{ user.lastName.charAt(0) }}
          </div>
          <div class="min-w-0 flex-1">
            <p class="truncate text-sm font-medium text-gray-900">
              {{ user.firstName }} {{ user.lastName }}
            </p>
            <p class="truncate text-xs text-gray-500">
              {{ roleLabels[user.role] ?? user.role }}
            </p>
          </div>
        </div>
      </div>
    </aside>

    <!-- ================================================================== -->
    <!-- MAIN CONTENT AREA                                                  -->
    <!-- Contains the top bar and the page content (<slot />).              -->
    <!-- ================================================================== -->
    <div class="flex flex-1 flex-col">
      <!-- Top navigation bar -->
      <header class="sticky top-0 z-20 flex h-16 items-center border-b border-gray-200 bg-white px-4 shadow-sm">
        <!-- Mobile menu button (hamburger) — opens the sidebar on small screens -->
        <button
          class="mr-3 rounded-lg p-2 text-gray-500 hover:bg-gray-100 hover:text-gray-700 lg:hidden"
          @click="toggleSidebar"
        >
          <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        </button>

        <!-- Logo (visible on all sizes) -->
        <NuxtLink to="/" class="text-lg font-bold text-gray-900">
          CatalogRO
        </NuxtLink>

        <!-- Spacer to push right-side items to the end -->
        <div class="flex-1" />

        <!-- Right side of top bar: sync status, user name, logout -->
        <div class="flex items-center gap-4">
          <!-- Sync status indicator (shows online/offline/syncing state) -->
          <SyncSyncStatus />

          <!-- Current user name + role badge (hidden on very small screens) -->
          <div v-if="user" class="hidden items-center gap-2 sm:flex">
            <span class="text-sm text-gray-600">
              {{ user.firstName }} {{ user.lastName }}
            </span>
            <span class="rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-500">
              {{ roleLabels[user.role] ?? user.role }}
            </span>
          </div>

          <!-- Logout button -->
          <button
            class="rounded-lg px-3 py-1.5 text-sm text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700"
            @click="handleLogout"
          >
            Ieșire
          </button>
        </div>
      </header>

      <!-- Page content (rendered by the current route's page component) -->
      <main class="flex-1 p-4 sm:p-6 lg:p-8">
        <slot />
      </main>
    </div>
  </div>
</template>
