<!--
  [classId].vue — Class catalog page with subject tabs and grade grid.

  This page is the main workspace for teachers. It shows:
  1. The class name and education level at the top
  2. A semester selector (Semestrul I / Semestrul II)
  3. Subject tabs — one tab for each subject the teacher teaches in this class
  4. The GradeGrid component for the selected subject + semester

  Route: /catalog/{classId}
  The classId comes from the URL parameter (dynamic route in Nuxt 3).

  Data flow:
  1. On mount, fetch class details from GET /classes/{classId}
  2. Fetch the teacher's subjects for this class from GET /classes/{classId}/teachers
  3. When a subject tab is clicked, the GradeGrid fetches grades automatically
  (it watches its props and refetches when they change)

  The page uses the default layout (sidebar + top bar).
-->

<script setup lang="ts">
import type {
  EducationLevel,
  Semester,
  TeacherClass,
  TeacherSubject,
} from '~/composables/useCatalog';

// ── Route parameters ───────────────────────────────────────────────────────

/**
 * Get the classId from the URL.
 * For the route /catalog/abc-123, this will be "abc-123".
 */
const route = useRoute();
const classId = computed(() => route.params.classId as string);

// ── Auth ───────────────────────────────────────────────────────────────────

/**
 * Get the current user — we need their ID to filter the teacher's subjects.
 */
const { user, isAuthenticated } = useAuth();

/**
 * Redirect to login if not authenticated.
 */
if (import.meta.client && !isAuthenticated.value) {
  void navigateTo('/login');
}

// ── Catalog data ───────────────────────────────────────────────────────────

/**
 * Get catalog functions to fetch class data and teacher subjects.
 */
const { fetchClasses, fetchTeacherSubjects, classes } = useCatalog();

// ── Local state ────────────────────────────────────────────────────────────

/** The class being viewed (fetched from the API) */
const currentClass = ref<TeacherClass | null>(null);

/** The subjects the current teacher teaches in this class */
const subjects = ref<TeacherSubject[]>([]);

/** The currently selected subject tab (its ID) */
const activeSubjectId = ref<string | null>(null);

/** The currently selected semester */
const activeSemester = ref<Semester>('I');

/** True while loading class and subject data */
const isLoading = ref(true);

/** Error message if class data fails to load */
const loadError = ref<string | null>(null);

// ── Computed properties ────────────────────────────────────────────────────

/**
 * The education level of the current class.
 * Determines whether the GradeGrid shows qualifiers or numeric grades.
 * Falls back to 'middle' if the class hasn't loaded yet.
 */
const educationLevel = computed<EducationLevel>(
  () => currentClass.value?.educationLevel ?? 'middle',
);

/**
 * The currently selected subject object (used for display in the header).
 */
const activeSubject = computed<TeacherSubject | null>(() => {
  if (activeSubjectId.value === null) return null;
  return subjects.value.find((s) => s.id === activeSubjectId.value) ?? null;
});

/**
 * Map education levels to human-readable Romanian labels.
 */
const educationLevelLabels: Record<string, string> = {
  primary: 'Primar',
  middle: 'Gimnaziu',
  high: 'Liceu',
};

// ── Data loading ───────────────────────────────────────────────────────────

/**
 * On mount: load the class details and the teacher's subject assignments.
 *
 * Steps:
 * 1. Fetch all classes (if not already loaded) to find the current class
 * 2. Look up the class by ID from the fetched list
 * 3. Fetch the teacher's subjects for this class
 * 4. Auto-select the first subject tab
 */
onMounted(async () => {
  isLoading.value = true;
  loadError.value = null;

  try {
    /* Fetch teacher's classes if not already loaded.
     * This populates the reactive `classes` ref in useCatalog. */
    if (classes.value.length === 0) {
      await fetchClasses();
    }

    /* Find the current class in the loaded list */
    const found = classes.value.find((c) => c.id === classId.value);

    if (found !== undefined) {
      currentClass.value = found;

      /* If the class already has subjects attached (from fetchClasses),
       * use those. Otherwise, fetch them separately. */
      if (found.subjects.length > 0) {
        subjects.value = found.subjects;
      } else if (user.value !== null) {
        subjects.value = await fetchTeacherSubjects(classId.value, user.value.id);
      }
    } else {
      /* Class not found — could be an invalid URL or teacher doesn't have access */
      loadError.value = 'Clasa nu a fost găsită sau nu aveți acces.';
    }

    /* Auto-select the first subject if there are any */
    if (subjects.value.length > 0 && subjects.value[0] !== undefined) {
      activeSubjectId.value = subjects.value[0].id;
    }
  } catch (e: unknown) {
    loadError.value = e instanceof Error ? e.message : 'Eroare la încărcarea datelor';
  } finally {
    isLoading.value = false;
  }
});

// ── Methods ────────────────────────────────────────────────────────────────

/**
 * Switch to a different subject tab.
 * The GradeGrid watches its props and will automatically refetch grades.
 *
 * @param subjectId - The ID of the subject to switch to
 */
function selectSubject(subjectId: string): void {
  activeSubjectId.value = subjectId;
}

/**
 * Switch to a different semester.
 * The GradeGrid watches its props and will automatically refetch grades.
 *
 * @param semester - 'I' or 'II'
 */
function selectSemester(semester: Semester): void {
  activeSemester.value = semester;
}
</script>

<template>
  <!-- ================================================================== -->
  <!-- LOADING STATE                                                      -->
  <!-- Shown while the class and subject data is being fetched.           -->
  <!-- ================================================================== -->
  <div data-testid="catalog-loading" v-if="isLoading" class="space-y-4">
    <div class="h-8 w-48 animate-pulse rounded bg-gray-200" />
    <div class="h-6 w-32 animate-pulse rounded bg-gray-200" />
    <div class="flex gap-2">
      <div v-for="n in 3" :key="n" class="h-10 w-24 animate-pulse rounded-lg bg-gray-200" />
    </div>
    <div class="h-64 animate-pulse rounded-xl bg-gray-200" />
  </div>

  <!-- ================================================================== -->
  <!-- ERROR STATE                                                        -->
  <!-- Shown if the class data failed to load.                            -->
  <!-- ================================================================== -->
  <div v-else-if="loadError !== null" class="space-y-4">
    <div data-testid="catalog-error" class="rounded-lg border border-red-200 bg-red-50 p-6 text-center">
      <p class="text-sm text-red-700">{{ loadError }}</p>
      <NuxtLink
        to="/"
        class="mt-3 inline-block text-sm font-medium text-blue-600 hover:text-blue-800"
      >
        &larr; Înapoi la tabloul de bord
      </NuxtLink>
    </div>
  </div>

  <!-- ================================================================== -->
  <!-- MAIN CATALOG VIEW                                                  -->
  <!-- Shows when class data has loaded successfully.                     -->
  <!-- ================================================================== -->
  <div v-else-if="currentClass !== null" class="space-y-6">
    <!-- ── Page header: class name, back link, semester selector ─────── -->
    <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <!-- Back to dashboard link -->
        <NuxtLink
          data-testid="back-link"
          to="/"
          class="mb-1 inline-flex items-center text-sm text-gray-500 hover:text-gray-700"
        >
          <svg
            class="mr-1 h-4 w-4"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="2"
          >
            <path stroke-linecap="round" stroke-linejoin="round" d="M15 19l-7-7 7-7" />
          </svg>
          Înapoi
        </NuxtLink>

        <!-- Class name and education level badge -->
        <h1 data-testid="class-title" class="text-2xl font-bold text-gray-900">Clasa {{ currentClass.name }}</h1>
        <p class="mt-0.5 text-sm text-gray-500">
          <span data-testid="education-level-badge">{{ educationLevelLabels[currentClass.educationLevel] ?? currentClass.educationLevel }}</span>
          &middot;
          <!-- studentCount may not be present in all API responses — guard with optional chaining -->
          <span v-if="currentClass.studentCount != null" data-testid="catalog-student-count">{{ currentClass.studentCount }} {{ currentClass.studentCount === 1 ? 'elev' : 'elevi' }}</span>
        </p>
      </div>

      <!-- Semester selector: two toggle buttons for Semester I and II -->
      <div class="flex rounded-lg border border-gray-200 bg-white p-1 shadow-sm">
        <button
          data-testid="semester-I"
          type="button"
          :class="[
            'rounded-md px-4 py-2 text-sm font-medium transition-colors',
            activeSemester === 'I'
              ? 'bg-blue-600 text-white shadow-sm'
              : 'text-gray-600 hover:bg-gray-50',
          ]"
          @click="selectSemester('I')"
        >
          Semestrul I
        </button>
        <button
          data-testid="semester-II"
          type="button"
          :class="[
            'rounded-md px-4 py-2 text-sm font-medium transition-colors',
            activeSemester === 'II'
              ? 'bg-blue-600 text-white shadow-sm'
              : 'text-gray-600 hover:bg-gray-50',
          ]"
          @click="selectSemester('II')"
        >
          Semestrul II
        </button>
      </div>
    </div>

    <!-- ── Subject tabs ─────────────────────────────────────────────── -->
    <!-- One tab for each subject the teacher teaches in this class.   -->
    <!-- Clicking a tab switches the GradeGrid to show that subject's  -->
    <!-- grades.                                                       -->
    <div v-if="subjects.length > 0" class="border-b border-gray-200">
      <nav class="-mb-px flex space-x-1 overflow-x-auto" role="tablist">
        <button
          data-testid="subject-tab"
          v-for="subject in subjects"
          :key="subject.id"
          type="button"
          role="tab"
          :aria-selected="activeSubjectId === subject.id"
          :class="[
            'whitespace-nowrap border-b-2 px-4 py-3 text-sm font-medium transition-colors',
            activeSubjectId === subject.id
              ? 'border-blue-500 text-blue-600'
              : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700',
          ]"
          @click="selectSubject(subject.id)"
        >
          {{ subject.name }}
          <!-- Show thesis indicator if the subject has a thesis exam -->
          <span v-if="subject.hasThesis" class="ml-1 text-xs text-gray-400" title="Materie cu teză">
            (T)
          </span>
        </button>
      </nav>
    </div>

    <!-- No subjects assigned to this teacher -->
    <div v-else class="rounded-xl border-2 border-dashed border-gray-300 p-8 text-center">
      <p class="text-sm text-gray-500">Nu aveți materii asignate la această clasă.</p>
      <NuxtLink
        to="/"
        class="mt-2 inline-block text-sm font-medium text-blue-600 hover:text-blue-800"
      >
        &larr; Înapoi la tabloul de bord
      </NuxtLink>
    </div>

    <!-- ── Grade grid ───────────────────────────────────────────────── -->
    <!-- The main grade table component, shown for the active subject.  -->
    <!-- It watches classId, subjectId, and semester and auto-refetches -->
    <!-- when any of them change.                                       -->
    <div data-testid="grade-grid-container" v-if="activeSubjectId !== null">
      <!-- Subject header: name and thesis indicator -->
      <div v-if="activeSubject !== null" class="mb-2 flex items-center gap-2">
        <h2 class="text-lg font-semibold text-gray-800">
          {{ activeSubject.name }}
        </h2>
        <span
          v-if="activeSubject.hasThesis"
          class="rounded bg-purple-50 px-2 py-0.5 text-xs font-medium text-purple-700"
        >
          Cu teză
        </span>
      </div>

      <!-- The GradeGrid component handles all the grade display + CRUD -->
      <CatalogGradeGrid
        :class-id="classId"
        :subject-id="activeSubjectId"
        :semester="activeSemester"
        :education-level="educationLevel"
      />
    </div>
  </div>
</template>
