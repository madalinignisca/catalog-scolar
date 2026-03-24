<!--
  GradeGrid.vue — THE core catalog component.

  This is the main grade entry grid that teachers use daily. It displays:
  - Rows: one row per student (sorted alphabetically by last name)
  - Columns: grade entries for the selected subject + semester
  - Last column: computed average (or qualifier summary for primary)

  Key features:
  - Click any existing grade to edit it (opens GradeInput in edit mode)
  - Click the "+" button on a student row to add a new grade
  - Shows grade value, date as tooltip, and description as tooltip
  - For primary level: displays qualifier badges (FB/B/S/I) with colors
  - For middle/high: displays numeric grades (1-10)
  - Average column shows the arithmetic mean (server-computed value)
  - Delete button on each grade (with confirmation)

  Props:
  - `classId` — which class we are viewing
  - `subjectId` — which subject's grades to show
  - `semester` — which semester (I or II)
  - `educationLevel` — determines grade display (qualifiers vs numeric)

  The component fetches grade data on mount and when props change,
  using the `useCatalog` composable.
-->

<script setup lang="ts">
import type {
  EducationLevel,
  Semester,
  Grade,
  StudentWithGrades,
  QualifierGrade,
} from '~/composables/useCatalog';

// ── Props ──────────────────────────────────────────────────────────────────

interface Props {
  /** The class whose students we display in the grid rows */
  classId: string;
  /** The subject whose grades we display in the grid columns */
  subjectId: string;
  /** Which semester to show grades for (I or II) */
  semester: Semester;
  /**
   * Education level determines the grading system:
   * - 'primary': qualifier grades (FB/B/S/I) with colored badges
   * - 'middle'/'high': numeric grades (1-10) with average column
   */
  educationLevel: EducationLevel;
}

const props = defineProps<Props>();

// ── Composables ────────────────────────────────────────────────────────────

/**
 * Get catalog data functions: grade grid, CRUD operations, optimistic updates.
 */
const {
  gradeGrid,
  isLoading,
  error,
  fetchClassGrades,
  addGrade,
  updateGrade,
  deleteGrade,
  addGradeToGrid,
  updateGradeInGrid,
  removeGradeFromGrid,
} = useCatalog();

// ── Local state ────────────────────────────────────────────────────────────

/** Whether the GradeInput modal is currently visible */
const isInputVisible = ref(false);

/** The student currently being graded (for the GradeInput modal) */
const activeStudentId = ref<string | null>(null);

/** The student's name (displayed in the GradeInput modal title) */
const activeStudentName = ref('');

/** If editing, this holds the existing grade; null for new grades */
const activeGrade = ref<Grade | null>(null);

/** Whether a save/delete operation is in progress (for loading indicators) */
const isSaving = ref(false);

/** ID of the grade being deleted (to show a spinner on that specific grade) */
const deletingGradeId = ref<string | null>(null);

// ── Data fetching ──────────────────────────────────────────────────────────

/**
 * Fetch grade data whenever the class, subject, or semester changes.
 * This is the core data loading mechanism for the grid.
 */
watch(
  () => [props.classId, props.subjectId, props.semester] as const,
  async ([classId, subjectId, semester]) => {
    await fetchClassGrades(classId, subjectId, semester);
  },
  { immediate: true },
);

// ── Computed properties ────────────────────────────────────────────────────

/**
 * Students sorted alphabetically by last name, then first name.
 * This ensures a consistent order in the grade grid.
 */
const sortedStudents = computed<StudentWithGrades[]>(() => {
  return [...gradeGrid.value].sort((a, b) => {
    /* Use || '' to guard against missing names from partial API responses.
     * The TypeScript type says these are non-optional, but the runtime API
     * may omit them if a student record is incomplete. */
    const lastNameCompare = (a.lastName || '').localeCompare(b.lastName || '', 'ro');
    if (lastNameCompare !== 0) return lastNameCompare;
    return (a.firstName || '').localeCompare(b.firstName || '', 'ro');
  });
});

/**
 * Whether we are using qualifier grades (primary school) or numeric (middle/high).
 */
const usesQualifiers = computed(() => props.educationLevel === 'primary');

// ── Grade display helpers ──────────────────────────────────────────────────

/**
 * Get the display value for a grade.
 * For numeric grades: returns the number as a string (e.g. "8").
 * For qualifier grades: returns the qualifier code (e.g. "FB").
 *
 * @param grade - The grade to display
 * @returns A string representation of the grade value
 */
function gradeDisplayValue(grade: Grade): string {
  // Use != null (loose equality) to catch both null AND undefined.
  // The API may omit numeric_grade entirely (undefined after snakeToCamel)
  // rather than sending it as null.
  if (grade.numericGrade != null) {
    return String(grade.numericGrade);
  }
  if (grade.qualifierGrade != null) {
    return grade.qualifierGrade;
  }
  return '—';
}

/**
 * Get Tailwind CSS classes for a qualifier badge color.
 * Each qualifier has a distinct color for quick visual recognition:
 * - FB (Foarte Bine) = green (excellent)
 * - B (Bine) = blue (good)
 * - S (Suficient) = yellow (acceptable)
 * - I (Insuficient) = red (failing)
 *
 * @param qualifier - The qualifier grade value
 * @returns Tailwind CSS class string for badge colors
 */
function qualifierColorClasses(qualifier: QualifierGrade): string {
  const colorMap: Record<QualifierGrade, string> = {
    FB: 'bg-green-100 text-green-800 border-green-200',
    B: 'bg-blue-100 text-blue-800 border-blue-200',
    S: 'bg-yellow-100 text-yellow-800 border-yellow-200',
    I: 'bg-red-100 text-red-800 border-red-200',
  };
  return colorMap[qualifier];
}

/**
 * Get Tailwind CSS classes for a numeric grade color.
 * Colors help teachers quickly spot low grades:
 * - 9-10 = green (excellent)
 * - 7-8 = blue (good)
 * - 5-6 = yellow (acceptable)
 * - 1-4 = red (failing, below minimum passing grade of 5)
 *
 * @param grade - The numeric grade value (1-10)
 * @returns Tailwind CSS class string for badge colors
 */
function numericGradeColorClasses(grade: number): string {
  if (grade >= 9) return 'bg-green-100 text-green-800 border-green-200';
  if (grade >= 7) return 'bg-blue-100 text-blue-800 border-blue-200';
  if (grade >= 5) return 'bg-yellow-100 text-yellow-800 border-yellow-200';
  return 'bg-red-100 text-red-800 border-red-200';
}

/**
 * Get the appropriate CSS classes for any grade badge (qualifier or numeric).
 *
 * @param grade - The grade object
 * @returns Tailwind CSS class string
 */
function gradeBadgeClasses(grade: Grade): string {
  if (grade.qualifierGrade != null) {
    return qualifierColorClasses(grade.qualifierGrade);
  }
  if (grade.numericGrade != null) {
    return numericGradeColorClasses(grade.numericGrade);
  }
  return 'bg-gray-100 text-gray-800 border-gray-200';
}

/**
 * Build a tooltip string for a grade showing date and description.
 * Displayed on hover so teachers can see context without opening the grade.
 *
 * @param grade - The grade to build the tooltip for
 * @returns A tooltip string like "15.10.2026 — Test la capitolul 3"
 */
function gradeTooltip(grade: Grade): string {
  /* Format date from ISO (2026-10-15) to Romanian format (15.10.2026).
   * Use || '' to guard against missing gradeDate from partial API responses. */
  const rawDate = grade.gradeDate || '';
  const parts = rawDate.split('-');
  const day = parts[2] ?? '';
  const month = parts[1] ?? '';
  const year = parts[0] ?? '';
  const formattedDate = parts.length === 3 ? `${day}.${month}.${year}` : rawDate;

  if (grade.description !== null && grade.description !== '') {
    return `${formattedDate} — ${grade.description}`;
  }
  return formattedDate;
}

/**
 * Format the average value for display.
 * Shows two decimal places for numeric averages.
 * Returns "—" if no average is computed yet.
 *
 * @param average - The computed average (null if not enough grades)
 * @returns Formatted average string
 */
function formatAverage(average: number | null): string {
  if (average === null) return '—';
  return average.toFixed(2);
}

// ── Grade CRUD actions ─────────────────────────────────────────────────────

/**
 * Open the GradeInput modal to add a new grade for a student.
 *
 * @param student - The student to add a grade for
 */
function openAddGrade(student: StudentWithGrades): void {
  activeStudentId.value = student.studentId;
  activeStudentName.value = `${student.lastName} ${student.firstName}`;
  activeGrade.value = null;
  isInputVisible.value = true;
}

/**
 * Open the GradeInput modal to edit an existing grade.
 *
 * @param student - The student who owns this grade
 * @param grade - The grade to edit
 */
function openEditGrade(student: StudentWithGrades, grade: Grade): void {
  activeStudentId.value = student.studentId;
  activeStudentName.value = `${student.lastName} ${student.firstName}`;
  activeGrade.value = grade;
  isInputVisible.value = true;
}

/**
 * Close the GradeInput modal without saving.
 */
function closeGradeInput(): void {
  isInputVisible.value = false;
  activeStudentId.value = null;
  activeGrade.value = null;
}

/**
 * Handle the save event from the GradeInput modal.
 * Creates a new grade or updates an existing one, then optimistically
 * updates the local grid so the UI reflects changes immediately.
 *
 * @param payload - The grade data from the GradeInput form
 */
async function handleSaveGrade(payload: {
  numericGrade?: number;
  qualifierGrade?: QualifierGrade;
  gradeDate: string;
  description: string;
}): Promise<void> {
  if (activeStudentId.value === null) return;

  isSaving.value = true;

  try {
    if (activeGrade.value !== null) {
      /* ── EDIT MODE: update an existing grade ── */
      const updated = await updateGrade(activeGrade.value.id, {
        numericGrade: payload.numericGrade,
        qualifierGrade: payload.qualifierGrade,
        gradeDate: payload.gradeDate,
        description: payload.description,
      });

      /* Optimistically update the grade in the local grid */
      updateGradeInGrid(activeGrade.value.id, {
        numericGrade: payload.numericGrade ?? null,
        qualifierGrade: payload.qualifierGrade ?? null,
        gradeDate: payload.gradeDate,
        description: payload.description,
        updatedAt: updated?.updatedAt ?? new Date().toISOString(),
      });
    } else {
      /* ── CREATE MODE: add a new grade ── */
      const created = await addGrade({
        studentId: activeStudentId.value,
        classId: props.classId,
        subjectId: props.subjectId,
        semester: props.semester,
        numericGrade: payload.numericGrade,
        qualifierGrade: payload.qualifierGrade,
        gradeDate: payload.gradeDate,
        description: payload.description,
      });

      /* Optimistically add the new grade to the local grid */
      if (created !== null) {
        addGradeToGrid(activeStudentId.value, created);
      }
    }

    /* Close the modal after successful save */
    closeGradeInput();
  } finally {
    isSaving.value = false;
  }
}

/**
 * Delete a grade after user confirmation.
 * Soft-deletes on the server (sets deleted_at) and removes from the local grid.
 *
 * @param grade - The grade to delete
 */
async function handleDeleteGrade(grade: Grade): Promise<void> {
  /* Ask for confirmation before deleting */
  if (!window.confirm('Sigur doriți să ștergeți această notă?')) {
    return;
  }

  deletingGradeId.value = grade.id;

  try {
    const success = await deleteGrade(grade.id);
    if (success) {
      /* Optimistically remove the grade from the local grid */
      removeGradeFromGrid(grade.id);
    }
  } finally {
    deletingGradeId.value = null;
  }
}
</script>

<template>
  <div class="space-y-4">
    <!-- Error banner -->
    <div
      data-testid="grade-grid-error"
      v-if="error !== null && error !== ''"
      class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700"
    >
      {{ error }}
    </div>

    <!-- Loading skeleton for the grade table -->
    <div data-testid="grade-grid-loading" v-if="isLoading" class="space-y-3">
      <div v-for="n in 5" :key="n" class="h-12 animate-pulse rounded-lg bg-gray-200" />
    </div>

    <!-- Empty state: no students in this class -->
    <div
      data-testid="grade-grid-empty"
      v-else-if="sortedStudents.length === 0"
      class="rounded-xl border-2 border-dashed border-gray-300 p-8 text-center"
    >
      <p class="text-sm text-gray-500">Nu există elevi înscriși în această clasă.</p>
    </div>

    <!-- ================================================================== -->
    <!-- GRADE TABLE                                                        -->
    <!-- The main grid: rows = students, columns = grades + average.        -->
    <!-- Responsive: horizontally scrollable on small screens.              -->
    <!-- ================================================================== -->
    <div v-else class="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
      <table data-testid="grade-grid" class="min-w-full divide-y divide-gray-200">
        <!-- Table header -->
        <thead class="bg-gray-50">
          <tr>
            <!-- Row number column -->
            <th
              scope="col"
              class="w-12 px-3 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500"
            >
              Nr.
            </th>
            <!-- Student name column -->
            <th
              scope="col"
              class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500"
            >
              Elev
            </th>
            <!-- Grades column (flexible width for all grade badges) -->
            <th
              scope="col"
              class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500"
            >
              {{ usesQualifiers ? 'Calificative' : 'Note' }}
            </th>
            <!-- Average column (only for numeric grades) -->
            <th
              v-if="!usesQualifiers"
              scope="col"
              class="w-24 px-4 py-3 text-center text-xs font-semibold uppercase tracking-wider text-gray-500"
            >
              Medie
            </th>
            <!-- Actions column (add grade button) -->
            <th
              scope="col"
              class="w-16 px-3 py-3 text-center text-xs font-semibold uppercase tracking-wider text-gray-500"
            >
              <span class="sr-only">Acțiuni</span>
            </th>
          </tr>
        </thead>

        <!-- Table body: one row per student -->
        <tbody class="divide-y divide-gray-100">
          <tr
            data-testid="student-row"
            v-for="(student, index) in sortedStudents"
            :key="student.studentId"
            class="transition-colors hover:bg-gray-50"
          >
            <!-- Row number -->
            <td class="whitespace-nowrap px-3 py-3 text-sm text-gray-400">
              {{ index + 1 }}
            </td>

            <!-- Student name -->
            <td data-testid="student-name" class="whitespace-nowrap px-4 py-3">
              <span class="text-sm font-medium text-gray-900">
                {{ student.lastName }}
              </span>
              <span class="ml-1 text-sm text-gray-600">
                {{ student.firstName }}
              </span>
            </td>

            <!-- Grade badges: each grade is a clickable badge -->
            <td class="px-4 py-3">
              <div class="flex flex-wrap items-center gap-1.5">
                <!-- Individual grade badges -->
                <div v-for="grade in student.grades" :key="grade.id" class="group relative">
                  <!-- Grade badge button: click to edit this grade -->
                  <button
                    data-testid="grade-badge"
                    type="button"
                    :title="gradeTooltip(grade)"
                    :disabled="deletingGradeId === grade.id"
                    :class="[
                      'inline-flex items-center rounded border px-2 py-0.5 text-xs font-semibold transition-all',
                      gradeBadgeClasses(grade),
                      'cursor-pointer hover:ring-2 hover:ring-blue-300',
                      grade.isThesis ? 'ring-1 ring-purple-400' : '',
                    ]"
                    @click="openEditGrade(student, grade)"
                  >
                    <!-- Thesis indicator: small "T" prefix for thesis grades -->
                    <span v-if="grade.isThesis" class="mr-0.5 text-purple-600">T</span>
                    {{ gradeDisplayValue(grade) }}
                  </button>

                  <!-- Delete button: appears on hover over the grade badge -->
                  <button
                    data-testid="delete-grade-button"
                    type="button"
                    title="Șterge nota"
                    class="absolute -right-1 -top-1 hidden h-4 w-4 items-center justify-center rounded-full bg-red-500 text-white group-hover:flex"
                    @click.stop="handleDeleteGrade(grade)"
                  >
                    <svg
                      class="h-2.5 w-2.5"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="3"
                    >
                      <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        d="M6 18L18 6M6 6l12 12"
                      />
                    </svg>
                  </button>
                </div>

                <!-- Empty state: no grades yet for this student -->
                <span
                  v-if="!student.grades || student.grades.length === 0"
                  class="text-xs italic text-gray-400"
                >
                  Nicio notă
                </span>
              </div>
            </td>

            <!-- Average column (only for numeric grades) -->
            <td v-if="!usesQualifiers" class="whitespace-nowrap px-4 py-3 text-center">
              <span
                data-testid="student-average"
                :class="[
                  'text-sm font-semibold',
                  student.average !== null && student.average < 5
                    ? 'text-red-600'
                    : student.average !== null && student.average >= 9
                      ? 'text-green-600'
                      : 'text-gray-700',
                ]"
              >
                {{ formatAverage(student.average) }}
              </span>
            </td>

            <!-- Add grade button -->
            <td class="whitespace-nowrap px-3 py-3 text-center">
              <button
                data-testid="add-grade-button"
                type="button"
                title="Adaugă notă"
                class="inline-flex h-7 w-7 items-center justify-center rounded-full bg-blue-50 text-blue-600 transition-colors hover:bg-blue-100"
                @click="openAddGrade(student)"
              >
                <svg
                  class="h-4 w-4"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <path stroke-linecap="round" stroke-linejoin="round" d="M12 4v16m8-8H4" />
                </svg>
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- ================================================================== -->
    <!-- GRADE INPUT MODAL                                                  -->
    <!-- Shown when the teacher clicks "add" or clicks an existing grade.   -->
    <!-- ================================================================== -->
    <CatalogGradeInput
      :visible="isInputVisible"
      :education-level="educationLevel"
      :existing-grade="activeGrade"
      :student-name="activeStudentName"
      @save="handleSaveGrade"
      @close="closeGradeInput"
    />
  </div>
</template>
