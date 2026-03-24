<!--
  index.vue — Role-based dashboard (home page).

  This is the main landing page after login. What the user sees depends
  on their role:

  - **Teacher**: a grid of class cards, each showing class name, education
    level, student count, and assigned subjects. Clicking a card navigates
    to the catalog page for that class (/catalog/{classId}).

  - **Admin**: quick-access cards for school administration (users, classes,
    reports). These link to admin pages (built separately).

  - **Parent**: placeholder for future "my children" view.

  - **Other roles**: a simple welcome message.

  If the user is not authenticated, they are redirected to /login.

  This page uses the default layout (sidebar + top bar) which is
  automatically applied by Nuxt since no `definePageMeta({ layout: false })`
  is set.
-->

<script setup lang="ts">
import type { TeacherClass } from '~/composables/useCatalog';

/**
 * Get the current user from the auth composable.
 * The `user` ref contains the logged-in user's profile (name, role, etc.).
 * `isAuthenticated` is a computed boolean for quick auth checks.
 */
const { user, isAuthenticated } = useAuth();

/**
 * Get catalog functions to fetch the teacher's class list.
 * `classes` is a reactive ref that holds the fetched TeacherClass array.
 * `fetchClasses` calls GET /classes and populates the ref.
 */
const { classes, fetchClasses, isLoading, error } = useCatalog();

/**
 * Track whether the initial page load is still in progress.
 * While true, the template shows a loading spinner instead of role-based content.
 * This prevents the "flash of fallback content" before fetchProfile completes.
 */
const pageLoading = ref(true);

/**
 * On mount: check authentication and load data.
 *
 * We do this in onMounted (not at setup time) because:
 * 1. localStorage is only available on the client
 * 2. fetchProfile() is async and needs to complete before we check isAuthenticated
 * 3. The auth state (user ref) may be empty if this is a fresh page load
 *    (e.g., after login redirected here, or browser refresh)
 */
onMounted(async () => {
  // If user state is empty but we have a token, try to restore the session
  // by fetching the profile from the API.
  const { fetchProfile } = useAuth();
  if (!isAuthenticated.value) {
    await fetchProfile();
  }

  // After attempting to restore, if still not authenticated → go to login
  if (!isAuthenticated.value) {
    void navigateTo('/login');
    return;
  }

  // If the user is a teacher, fetch their assigned classes for the dashboard
  if (user.value?.role === 'teacher') {
    await fetchClasses();
  }

  // Page is ready — show the role-based content
  pageLoading.value = false;
});

/**
 * Map education levels to human-readable Romanian labels.
 * Used to display on the class cards so teachers know the grading system.
 */
const educationLevelLabels: Record<string, string> = {
  primary: 'Primar (P-IV)',
  middle: 'Gimnaziu (V-VIII)',
  high: 'Liceu (IX-XII)',
};

/**
 * Navigate to the catalog page for a specific class.
 * The catalog page shows the grade grid for the class.
 *
 * @param classItem - The class to navigate to
 */
function openClass(classItem: TeacherClass): void {
  void navigateTo(`/catalog/${classItem.id}`);
}
</script>

<template>
  <!-- ================================================================== -->
  <!-- LOADING STATE: shown while fetchProfile is in progress             -->
  <!-- ================================================================== -->
  <div data-testid="dashboard-loading" v-if="pageLoading" class="flex items-center justify-center py-20">
    <div class="text-center">
      <div
        class="mx-auto h-8 w-8 animate-spin rounded-full border-4 border-blue-200 border-t-blue-600"
      />
      <p class="mt-4 text-sm text-gray-500">Se încarcă...</p>
    </div>
  </div>

  <!-- ================================================================== -->
  <!-- TEACHER DASHBOARD                                                  -->
  <!-- Shows a grid of class cards when the user is a teacher.            -->
  <!-- ================================================================== -->
  <div data-testid="dashboard-content" v-else-if="user?.role === 'teacher'" class="space-y-6">
    <!-- Page heading -->
    <div>
      <h1 class="text-2xl font-bold text-gray-900">Tablou de bord</h1>
      <p class="mt-1 text-sm text-gray-500">Clasele la care predați în anul școlar curent</p>
    </div>

    <!-- Error banner: shown if the class list failed to load -->
    <div
      data-testid="dashboard-error"
      v-if="error !== null && error !== ''"
      class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700"
    >
      {{ error }}
    </div>

    <!-- Loading state: skeleton cards while fetching -->
    <div v-if="isLoading" class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <div
        v-for="n in 3"
        :key="n"
        class="animate-pulse rounded-xl border border-gray-200 bg-white p-6 shadow-sm"
      >
        <div class="mb-3 h-5 w-24 rounded bg-gray-200" />
        <div class="mb-2 h-4 w-32 rounded bg-gray-100" />
        <div class="h-4 w-20 rounded bg-gray-100" />
      </div>
    </div>

    <!-- Empty state: no classes assigned to this teacher -->
    <div
      v-else-if="classes?.length === 0 && error === null"
      class="rounded-xl border-2 border-dashed border-gray-300 p-12 text-center"
    >
      <svg
        class="mx-auto h-12 w-12 text-gray-400"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="1"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M12 6.253v13m0-13C10.832 5.477 9.246 5 7.5 5S4.168 5.477 3 6.253v13C4.168 18.477 5.754 18 7.5 18s3.332.477 4.5 1.253m0-13C13.168 5.477 14.754 5 16.5 5c1.747 0 3.332.477 4.5 1.253v13C19.832 18.477 18.247 18 16.5 18c-1.746 0-3.332.477-4.5 1.253"
        />
      </svg>
      <h3 class="mt-4 text-sm font-semibold text-gray-900">Nicio clasă asignată</h3>
      <p class="mt-1 text-sm text-gray-500">
        Contactați secretariatul pentru a fi repartizat la clase.
      </p>
    </div>

    <!-- Class cards grid: one card per assigned class -->
    <div v-else class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <button
        data-testid="class-card"
        v-for="classItem in classes"
        :key="classItem.id"
        type="button"
        class="group rounded-xl border border-gray-200 bg-white p-6 text-left shadow-sm transition-all hover:border-blue-300 hover:shadow-md"
        @click="openClass(classItem)"
      >
        <!-- Class name and education level badge -->
        <div class="flex items-start justify-between">
          <h3 data-testid="class-card-name" class="text-lg font-semibold text-gray-900 group-hover:text-blue-700">
            Clasa {{ classItem.name }}
          </h3>
          <span class="rounded-full bg-blue-50 px-2 py-0.5 text-xs font-medium text-blue-700">
            {{ educationLevelLabels[classItem.educationLevel] ?? classItem.educationLevel }}
          </span>
        </div>

        <!-- Student count (may come as studentCount or maxStudents depending on API) -->
        <p v-if="classItem.studentCount || classItem.maxStudents" class="mt-2 text-sm text-gray-500">
          <span data-testid="class-card-student-count" class="font-medium text-gray-700">{{ classItem.studentCount ?? classItem.maxStudents }}</span>
          {{ (classItem.studentCount ?? classItem.maxStudents) === 1 ? 'elev' : 'elevi' }}
        </p>

        <!-- Subjects the teacher teaches in this class (may not be loaded yet) -->
        <div v-if="classItem.subjects?.length > 0" class="mt-3">
          <p class="mb-1 text-xs font-medium uppercase tracking-wide text-gray-400">Materii</p>
          <div class="flex flex-wrap gap-1.5">
            <span
              v-for="subject in classItem.subjects"
              :key="subject.id"
              class="rounded bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-600"
            >
              {{ subject.shortName ?? subject.name }}
            </span>
          </div>
        </div>

        <!-- Visual indicator to show this is clickable -->
        <div
          class="mt-4 flex items-center text-xs font-medium text-blue-600 opacity-0 transition-opacity group-hover:opacity-100"
        >
          Deschide catalogul
          <svg
            class="ml-1 h-3 w-3"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="2"
          >
            <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
          </svg>
        </div>
      </button>
    </div>
  </div>

  <!-- ================================================================== -->
  <!-- ADMIN DASHBOARD                                                    -->
  <!-- Quick-access cards for school administration features.             -->
  <!-- ================================================================== -->
  <div v-else-if="user?.role === 'admin'" class="space-y-6">
    <div>
      <h1 class="text-2xl font-bold text-gray-900">Administrare școală</h1>
      <p class="mt-1 text-sm text-gray-500">Gestionați utilizatori, clase și configurări</p>
    </div>

    <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <!-- Users management card -->
      <NuxtLink
        data-testid="admin-card"
        to="/admin/users"
        class="rounded-xl border border-gray-200 bg-white p-6 shadow-sm transition-shadow hover:shadow-md"
      >
        <h3 class="font-semibold text-gray-900">Utilizatori</h3>
        <p class="mt-1 text-sm text-gray-500">Provizionare conturi, activări în așteptare</p>
      </NuxtLink>

      <!-- Classes management card -->
      <NuxtLink
        data-testid="admin-card"
        to="/admin/classes"
        class="rounded-xl border border-gray-200 bg-white p-6 shadow-sm transition-shadow hover:shadow-md"
      >
        <h3 class="font-semibold text-gray-900">Clase &amp; Materii</h3>
        <p class="mt-1 text-sm text-gray-500">Încadrare, formațiuni de studiu</p>
      </NuxtLink>

      <!-- Reports card -->
      <NuxtLink
        data-testid="admin-card"
        to="/reports"
        class="rounded-xl border border-gray-200 bg-white p-6 shadow-sm transition-shadow hover:shadow-md"
      >
        <h3 class="font-semibold text-gray-900">Rapoarte</h3>
        <p class="mt-1 text-sm text-gray-500">Dashboard, statistici, export ISJ</p>
      </NuxtLink>
    </div>
  </div>

  <!-- ================================================================== -->
  <!-- PARENT DASHBOARD (placeholder for future implementation)           -->
  <!-- ================================================================== -->
  <div v-else-if="user?.role === 'parent'" class="space-y-6">
    <div>
      <h1 class="text-2xl font-bold text-gray-900">Copiii mei</h1>
      <p class="mt-1 text-sm text-gray-500">Vizualizați situația școlară a copiilor</p>
    </div>
    <p class="text-gray-500">Încărcare date...</p>
  </div>

  <!-- ================================================================== -->
  <!-- FALLBACK: unknown or unhandled role                                -->
  <!-- ================================================================== -->
  <div data-testid="welcome-message" v-else class="py-12 text-center text-gray-500">
    <p>Bine ați venit în CatalogRO</p>
  </div>
</template>
