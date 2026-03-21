<!--
  GradeInput.vue — Modal form for adding or editing a grade.

  This component shows a form where the teacher can:
  - Enter a numeric grade (1-10) for middle/high school students
  - Select a qualifier (FB/B/S/I) for primary school students
  - Set the date the grade was given
  - Add an optional description (observation about the grade)

  The form validates input before submitting:
  - Numeric grades must be integers between 1 and 10
  - Qualifier grades must be one of FB, B, S, or I
  - Date is required

  Props:
  - `visible` — controls whether the modal is shown
  - `educationLevel` — determines whether to show numeric or qualifier input
  - `existingGrade` — if provided, we are editing (pre-fills the form)
  - `studentName` — displayed in the modal title for context

  Emits:
  - `save` — when the form is submitted with valid data
  - `close` — when the modal is dismissed (cancel or backdrop click)
-->

<script setup lang="ts">
import type { QualifierGrade, EducationLevel, Grade } from '~/composables/useCatalog';

// ── Props ──────────────────────────────────────────────────────────────────

interface Props {
  /** Whether the modal is currently visible */
  visible: boolean;
  /**
   * Education level of the class.
   * 'primary' shows qualifier picker (FB/B/S/I).
   * 'middle' or 'high' shows numeric input (1-10).
   */
  educationLevel: EducationLevel;
  /**
   * If we are editing an existing grade, pass it here.
   * The form will pre-fill with the grade's current values.
   * If null/undefined, we are creating a new grade.
   */
  existingGrade?: Grade | null;
  /** Student's full name, shown in the modal title for context */
  studentName: string;
}

const props = withDefaults(defineProps<Props>(), {
  existingGrade: null,
});

// ── Emits ──────────────────────────────────────────────────────────────────

interface SavePayload {
  /** Numeric grade value (1-10), provided for middle/high school */
  numericGrade?: number;
  /** Qualifier value (FB/B/S/I), provided for primary school */
  qualifierGrade?: QualifierGrade;
  /** Date the grade was given (ISO date string) */
  gradeDate: string;
  /** Optional teacher observation/description */
  description: string;
}

const emit = defineEmits<{
  /** Fired when the teacher saves a valid grade */
  save: [payload: SavePayload];
  /** Fired when the modal is closed without saving */
  close: [];
}>();

// ── Form state ─────────────────────────────────────────────────────────────

/**
 * The numeric grade value entered by the teacher (1-10).
 * Only used when educationLevel is 'middle' or 'high'.
 */
const numericValue = ref<number | null>(null);

/**
 * The qualifier grade selected by the teacher (FB/B/S/I).
 * Only used when educationLevel is 'primary'.
 */
const qualifierValue = ref<QualifierGrade | null>(null);

/**
 * The date the grade was given.
 * Defaults to today's date in YYYY-MM-DD format.
 */
const gradeDate = ref(new Date().toISOString().split('T')[0]);

/**
 * Optional description/observation about the grade.
 * For example: "Test la capitolul 3" or "Tema de casă".
 */
const description = ref('');

/**
 * Validation error message, shown below the form when input is invalid.
 */
const validationError = ref('');

// ── Available qualifier options ────────────────────────────────────────────

/**
 * The four qualifier grades used in Romanian primary schools.
 * FB = Foarte Bine (Very Good), B = Bine (Good),
 * S = Suficient (Sufficient), I = Insuficient (Insufficient).
 */
const qualifierOptions: Array<{ value: QualifierGrade; label: string }> = [
  { value: 'FB', label: 'FB — Foarte Bine' },
  { value: 'B', label: 'B — Bine' },
  { value: 'S', label: 'S — Suficient' },
  { value: 'I', label: 'I — Insuficient' },
];

// ── Computed properties ────────────────────────────────────────────────────

/**
 * Whether we are editing an existing grade (vs creating a new one).
 * Controls the modal title and submit button text.
 */
const isEditing = computed(() => props.existingGrade !== null);

/**
 * Whether to show the qualifier picker (primary) or numeric input (middle/high).
 */
const usesQualifiers = computed(() => props.educationLevel === 'primary');

/**
 * Modal title changes based on whether we are adding or editing.
 */
const modalTitle = computed(() => (isEditing.value ? 'Modifică nota' : 'Adaugă notă'));

// ── Watchers ───────────────────────────────────────────────────────────────

/**
 * When the modal becomes visible, reset the form fields.
 * If editing an existing grade, pre-fill with its current values.
 * If adding a new grade, reset to defaults.
 */
watch(
  () => props.visible,
  (nowVisible) => {
    if (nowVisible) {
      validationError.value = '';

      if (props.existingGrade !== null) {
        /* Pre-fill form with existing grade data for editing */
        numericValue.value = props.existingGrade.numericGrade;
        qualifierValue.value = props.existingGrade.qualifierGrade;
        gradeDate.value = props.existingGrade.gradeDate;
        description.value = props.existingGrade.description ?? '';
      } else {
        /* Reset to defaults for a new grade */
        numericValue.value = null;
        qualifierValue.value = null;
        gradeDate.value = new Date().toISOString().split('T')[0];
        description.value = '';
      }
    }
  },
);

// ── Methods ────────────────────────────────────────────────────────────────

/**
 * Validate the form and emit the save event if valid.
 *
 * Validation rules:
 * - For numeric grades: must be an integer between 1 and 10
 * - For qualifier grades: must be one of FB, B, S, I
 * - Date is always required
 */
function handleSave(): void {
  validationError.value = '';

  /* Validate date is provided */
  if (gradeDate.value === '') {
    validationError.value = 'Data este obligatorie';
    return;
  }

  if (usesQualifiers.value) {
    /* PRIMARY SCHOOL: validate qualifier selection */
    if (qualifierValue.value === null) {
      validationError.value = 'Selectați un calificativ (FB, B, S sau I)';
      return;
    }

    emit('save', {
      qualifierGrade: qualifierValue.value,
      gradeDate: gradeDate.value,
      description: description.value,
    });
  } else {
    /* MIDDLE/HIGH SCHOOL: validate numeric grade */
    if (numericValue.value === null) {
      validationError.value = 'Introduceți o notă de la 1 la 10';
      return;
    }

    const grade = Math.round(numericValue.value);
    if (grade < 1 || grade > 10) {
      validationError.value = 'Nota trebuie să fie între 1 și 10';
      return;
    }

    emit('save', {
      numericGrade: grade,
      gradeDate: gradeDate.value,
      description: description.value,
    });
  }
}

/**
 * Close the modal without saving.
 */
function handleClose(): void {
  emit('close');
}
</script>

<template>
  <!-- Modal backdrop + container. Only rendered when `visible` is true. -->
  <Teleport to="body">
    <div v-if="visible" class="fixed inset-0 z-50 flex items-center justify-center p-4">
      <!-- Backdrop: semi-transparent overlay. Clicking it closes the modal. -->
      <button
        type="button"
        aria-label="Închide"
        class="fixed inset-0 cursor-default border-none bg-black/50"
        @click="handleClose"
      />

      <!-- Modal panel -->
      <div
        class="relative z-10 w-full max-w-md rounded-xl bg-white p-6 shadow-xl"
        role="dialog"
        aria-modal="true"
        :aria-label="modalTitle"
      >
        <!-- Modal header -->
        <div class="mb-5">
          <h2 class="text-lg font-semibold text-gray-900">
            {{ modalTitle }}
          </h2>
          <p class="mt-0.5 text-sm text-gray-500">
            {{ studentName }}
          </p>
        </div>

        <!-- Grade input form -->
        <form class="space-y-4" @submit.prevent="handleSave">
          <!-- ============================================================ -->
          <!-- QUALIFIER PICKER (primary school: FB / B / S / I)            -->
          <!-- Shown when the class education level is 'primary'.           -->
          <!-- Teachers select one of four qualifiers instead of a number.  -->
          <!-- ============================================================ -->
          <fieldset v-if="usesQualifiers">
            <legend class="mb-2 block text-sm font-medium text-gray-700">Calificativ</legend>
            <div class="grid grid-cols-2 gap-2">
              <button
                v-for="option in qualifierOptions"
                :key="option.value"
                type="button"
                :class="[
                  'rounded-lg border-2 px-4 py-3 text-sm font-semibold transition-colors',
                  qualifierValue === option.value
                    ? 'border-blue-500 bg-blue-50 text-blue-700'
                    : 'border-gray-200 bg-white text-gray-700 hover:border-gray-300 hover:bg-gray-50',
                ]"
                @click="qualifierValue = option.value"
              >
                {{ option.label }}
              </button>
            </div>
          </fieldset>

          <!-- ============================================================ -->
          <!-- NUMERIC GRADE INPUT (middle/high school: 1-10)               -->
          <!-- Shown when the class education level is 'middle' or 'high'.  -->
          <!-- Teachers type a number between 1 and 10.                     -->
          <!-- ============================================================ -->
          <div v-else>
            <label for="grade-numeric" class="mb-1 block text-sm font-medium text-gray-700">
              Notă (1-10)
            </label>
            <input
              id="grade-numeric"
              v-model.number="numericValue"
              type="number"
              min="1"
              max="10"
              step="1"
              placeholder="Introduceți nota"
              class="block w-full rounded-lg border border-gray-300 px-3 py-2 text-lg shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>

          <!-- ============================================================ -->
          <!-- DATE PICKER                                                  -->
          <!-- The date when the grade was given. Defaults to today.        -->
          <!-- ============================================================ -->
          <div>
            <label for="grade-date" class="mb-1 block text-sm font-medium text-gray-700">
              Data
            </label>
            <input
              id="grade-date"
              v-model="gradeDate"
              type="date"
              required
              class="block w-full rounded-lg border border-gray-300 px-3 py-2 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>

          <!-- ============================================================ -->
          <!-- DESCRIPTION (optional observation)                           -->
          <!-- The teacher can add a note about what this grade is for,     -->
          <!-- e.g. "Test la capitolul 3" or "Proiect individual".          -->
          <!-- ============================================================ -->
          <div>
            <label for="grade-description" class="mb-1 block text-sm font-medium text-gray-700">
              Descriere
              <span class="font-normal text-gray-400">(opțional)</span>
            </label>
            <input
              id="grade-description"
              v-model="description"
              type="text"
              placeholder="ex: Test la capitolul 3"
              class="block w-full rounded-lg border border-gray-300 px-3 py-2 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>

          <!-- Validation error message -->
          <div v-if="validationError !== ''" class="rounded-lg bg-red-50 p-3 text-sm text-red-700">
            {{ validationError }}
          </div>

          <!-- ============================================================ -->
          <!-- ACTION BUTTONS: Cancel and Save                              -->
          <!-- ============================================================ -->
          <div class="flex items-center justify-end gap-3 pt-2">
            <button
              type="button"
              class="rounded-lg px-4 py-2 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-100"
              @click="handleClose"
            >
              Anulează
            </button>
            <button
              type="submit"
              class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white shadow-sm transition-colors hover:bg-blue-700"
            >
              {{ isEditing ? 'Salvează modificarea' : 'Adaugă nota' }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </Teleport>
</template>
