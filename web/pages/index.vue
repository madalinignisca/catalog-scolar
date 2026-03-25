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
import { api } from '~/lib/api';

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
 * Represents a single child linked to a parent account.
 * Fields are camelCase after the snakeToCamel conversion in api().
 *
 * Mirrors the API response from GET /users/me/children:
 *   id                  — UUID of the child's user record
 *   firstName           — Child's given name
 *   lastName            — Child's family name
 *   email               — Child's school email address
 *   role                — Always "student" for linked children
 *   classId             — UUID of the class the child is enrolled in
 *   className           — Human-readable class name (e.g. "2A")
 *   classEducationLevel — "primary" | "middle" | "high"
 */
interface Child {
  id: string;
  firstName: string;
  lastName: string;
  email: string;
  role: 'student';
  classId: string | null;
  className: string | null;
  classEducationLevel: 'primary' | 'middle' | 'high' | null;
}

/**
 * Reactive list of children linked to the currently logged-in parent.
 * Populated by fetchChildren() during onMounted. Empty array before fetch.
 */
const children = ref<Child[]>([]);

/**
 * Loading flag for the children fetch.
 * True while the GET /users/me/children request is in-flight.
 * Drives the skeleton loading state in the parent dashboard template.
 */
const childrenLoading = ref(false);

/**
 * Error message from the children fetch, or null if the request succeeded.
 * Shown as a red error banner in the parent dashboard template.
 */
const childrenError = ref<string | null>(null);

/**
 * Fetches the list of children linked to the logged-in parent account.
 *
 * Calls GET /api/v1/users/me/children. The api() wrapper:
 *   1. Sends the httpOnly auth cookie automatically (credentials: 'include')
 *   2. Unwraps the { "data": [...] } envelope
 *   3. Converts snake_case keys to camelCase (first_name → firstName, etc.)
 *
 * Sets `childrenLoading` to true while the request is in-flight so the
 * template can show a skeleton placeholder instead of an empty screen.
 * On success, `children` is populated. On failure, `childrenError` is set.
 */
async function fetchChildren(): Promise<void> {
  childrenLoading.value = true;
  childrenError.value = null;
  try {
    // The endpoint returns an array of child user objects wrapped in { data: [...] }.
    // api<Child[]>() handles the envelope unwrap and camelCase conversion for us.
    children.value = await api<Child[]>('/users/me/children');
  } catch (err: unknown) {
    // Surface a human-readable message in the template error banner.
    // If the error is an Error instance, use its message; otherwise use a generic fallback.
    childrenError.value = err instanceof Error ? err.message : 'Eroare la încărcarea datelor.';
  } finally {
    // Always clear the loading state, even if the request failed,
    // so the spinner does not spin forever on network errors.
    childrenLoading.value = false;
  }
}

/**
 * On mount: check authentication and load data.
 *
 * With cookie-based auth, we don't need to check localStorage for a token.
 * Instead, we just call fetchProfile() — the httpOnly cookie is sent
 * automatically with the request (via credentials: 'include' in the api()
 * wrapper). If the cookie is missing or expired, fetchProfile() sets
 * user to null and we redirect to /login.
 */
onMounted(async () => {
  // If user state is empty, try to restore the session by fetching the
  // profile from the API. The httpOnly cookie handles authentication.
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

  // If the user is a parent, fetch their linked children for the dashboard.
  // This call is intentionally NOT awaited before setting pageLoading = false
  // so the page skeleton appears immediately and children load in the background.
  if (user.value?.role === 'parent') {
    void fetchChildren();
  }

  // Page is ready — show the role-based content
  pageLoading.value = false;
});

/**
 * Map education levels to human-readable Romanian labels.
 * Used on class cards (teacher view) and child cards (parent view) so
 * users always see a descriptive label instead of a raw enum value.
 *
 * primary → "Primar (P-IV)"   — classes P through IV, qualifier-based grading
 * middle  → "Gimnaziu (V-VIII)" — classes V-VIII, numeric grades 1–10
 * high    → "Liceu (IX-XII)"    — classes IX-XII, same rules as middle school
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
  <div
    data-testid="dashboard-loading"
    v-if="pageLoading"
    class="flex items-center justify-center py-20"
  >
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
      data-testid="empty-state"
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
          <h3
            data-testid="class-card-name"
            class="text-lg font-semibold text-gray-900 group-hover:text-blue-700"
          >
            Clasa {{ classItem.name }}
          </h3>
          <span class="rounded-full bg-blue-50 px-2 py-0.5 text-xs font-medium text-blue-700">
            {{ educationLevelLabels[classItem.educationLevel] ?? classItem.educationLevel }}
          </span>
        </div>

        <!-- Student count (may come as studentCount or maxStudents depending on API) -->
        <p
          v-if="classItem.studentCount || classItem.maxStudents"
          class="mt-2 text-sm text-gray-500"
        >
          <span data-testid="class-card-student-count" class="font-medium text-gray-700">{{
            classItem.studentCount ?? classItem.maxStudents
          }}</span>
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
  <div data-testid="dashboard-content" v-else-if="user?.role === 'admin'" class="space-y-6">
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
  <!-- PARENT DASHBOARD                                                   -->
  <!-- Shows a grid of child cards when the user is a parent.            -->
  <!-- Each card displays the child's name, class, and education level.  -->
  <!-- ================================================================== -->
  <div data-testid="dashboard-content" v-else-if="user?.role === 'parent'" class="space-y-6">
    <!-- Page heading -->
    <div>
      <h1 class="text-2xl font-bold text-gray-900">Copiii mei</h1>
      <p class="mt-1 text-sm text-gray-500">Vizualizați situația școlară a copiilor</p>
    </div>

    <!-- Error banner: shown if the children list failed to load -->
    <div
      v-if="childrenError !== null"
      class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700"
    >
      {{ childrenError }}
    </div>

    <!-- Loading state: skeleton cards while fetching children from the API -->
    <div v-if="childrenLoading" class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <!--
        Three placeholder skeleton cards are shown while the API call is in-flight.
        The animate-pulse class makes them fade in and out to indicate activity.
        The gray boxes represent the child name, class name, and badge areas.
      -->
      <div
        v-for="n in 3"
        :key="n"
        class="animate-pulse rounded-xl border border-gray-200 bg-white p-6 shadow-sm"
      >
        <div class="mb-3 h-5 w-36 rounded bg-gray-200" />
        <div class="flex items-center gap-2">
          <div class="h-4 w-20 rounded bg-gray-100" />
          <div class="h-4 w-24 rounded bg-gray-100" />
        </div>
      </div>
    </div>

    <!-- Empty state: parent has no children linked to their account yet -->
    <div
      v-else-if="children.length === 0 && childrenError === null"
      class="rounded-xl border-2 border-dashed border-gray-300 p-12 text-center"
    >
      <!--
        This state appears when the parent account exists but no student accounts
        have been linked to it yet. The secretary must perform the linkage
        via the admin user-provisioning flow.
      -->
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
          d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0z"
        />
      </svg>
      <h3 class="mt-4 text-sm font-semibold text-gray-900">Niciun copil asociat</h3>
      <p class="mt-1 text-sm text-gray-500">
        Contactați secretariatul școlii pentru a asocia contul copilului.
      </p>
    </div>

    <!-- Child cards grid: one card per linked child -->
    <div v-else class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <!--
        Each card shows a single linked child. The data-testid="child-card"
        attribute is required by the E2E test suite (parent.spec.ts, test 26)
        to locate and assert on individual child cards.
      -->
      <div
        data-testid="child-card"
        v-for="child in children"
        :key="child.id"
        class="rounded-xl border border-gray-200 bg-white p-6 shadow-sm"
      >
        <!-- Child's full name: last name first, matching Romanian convention -->
        <h3 class="text-lg font-semibold text-gray-900">
          {{ child.lastName }} {{ child.firstName }}
        </h3>

        <!-- Class name and education level badge -->
        <div class="mt-2 flex items-center gap-2">
          <!--
            Class name: e.g. "Clasa 2A".
            The "Clasa" prefix is added here (not stored in the API) to make
            the label read naturally in Romanian (e.g. "Clasa 2A").
          -->
          <span class="text-sm text-gray-500">Clasa {{ child.className }}</span>

          <!--
            Education level badge: maps the raw enum value ("primary", "middle",
            "high") to a human-readable Romanian label using educationLevelLabels.
            Falls back to the raw value if an unknown level is returned.
          -->
          <span class="rounded-full bg-blue-50 px-2 py-0.5 text-xs font-medium text-blue-700">
            {{ educationLevelLabels[child.classEducationLevel] ?? child.classEducationLevel }}
          </span>
        </div>
      </div>
    </div>
  </div>

  <!-- ================================================================== -->
  <!-- FALLBACK: unknown or unhandled role                                -->
  <!-- ================================================================== -->
  <div data-testid="welcome-message" v-else class="py-12 text-center text-gray-500">
    <p>Bine ați venit în CatalogRO</p>
  </div>
</template>
