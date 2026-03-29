<!--
  EvaluationList.vue — Descriptive evaluation list for primary school classes.

  Displays all students in a class with their descriptive evaluation text.
  Teachers can click on a student to write or edit their evaluation in an
  inline textarea. The API uses upsert, so saving always works whether
  creating or updating.

  Props:
  - classId: UUID of the class
  - subjectId: UUID of the subject
  - semester: 'I' or 'II'

  Used only for primary education (classes P-IV). The parent page shows
  this component instead of GradeGrid when the education level is 'primary'
  and the teacher switches to the "Evaluări" tab.
-->

<script setup lang="ts">
import type { Semester, StudentEvaluation } from '~/composables/useCatalog';

const props = defineProps<{
  classId: string;
  subjectId: string;
  semester: Semester;
}>();

const { fetchEvaluations, saveEvaluation } = useCatalog();

const students = ref<StudentEvaluation[]>([]);
const isLoading = ref(true);
const error = ref<string | null>(null);

/** Which student is currently being edited (by student ID) */
const editingStudentId = ref<string | null>(null);

/** The textarea content for the student being edited */
const editContent = ref('');

/** True while saving an evaluation */
const isSaving = ref(false);

/** Success message shown briefly after saving */
const savedStudentId = ref<string | null>(null);

// ── Load evaluations ──────────────────────────────────────────────────────

async function loadEvaluations(): Promise<void> {
  isLoading.value = true;
  error.value = null;
  try {
    students.value = await fetchEvaluations(props.classId, props.subjectId, props.semester);
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Eroare la încărcarea evaluărilor';
  } finally {
    isLoading.value = false;
  }
}

// Watch props and reload when they change
watch(() => [props.classId, props.subjectId, props.semester], loadEvaluations, { immediate: true });

// ── Edit / Save ───────────────────────────────────────────────────────────

function startEdit(studentId: string, currentContent: string): void {
  editingStudentId.value = studentId;
  editContent.value = currentContent;
}

function cancelEdit(): void {
  editingStudentId.value = null;
  editContent.value = '';
}

async function save(studentId: string): Promise<void> {
  if (editContent.value.trim() === '') return;

  isSaving.value = true;
  try {
    await saveEvaluation({
      studentId,
      classId: props.classId,
      subjectId: props.subjectId,
      semester: props.semester,
      content: editContent.value.trim(),
    });

    // Reload to get the updated data
    await loadEvaluations();

    editingStudentId.value = null;
    editContent.value = '';

    // Show success briefly
    savedStudentId.value = studentId;
    setTimeout(() => {
      savedStudentId.value = null;
    }, 2000);
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Eroare la salvare';
  } finally {
    isSaving.value = false;
  }
}
</script>

<template>
  <!-- Loading -->
  <div v-if="isLoading" data-testid="evaluation-loading" class="space-y-3">
    <div v-for="n in 5" :key="n" class="h-20 animate-pulse rounded-lg bg-gray-100" />
  </div>

  <!-- Error -->
  <div
    v-else-if="error !== null && error !== ''"
    data-testid="evaluation-error"
    class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700"
  >
    {{ error }}
  </div>

  <!-- Student list -->
  <div v-else class="space-y-3">
    <div v-if="students.length === 0" class="py-8 text-center text-sm text-gray-500">
      Nu sunt elevi înscriși în această clasă.
    </div>

    <div
      v-for="item in students"
      :key="item.student.id"
      data-testid="evaluation-card"
      class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm transition-shadow hover:shadow-md"
    >
      <!-- Student name + saved indicator -->
      <div class="mb-2 flex items-center justify-between">
        <h3 class="font-medium text-gray-900" data-testid="evaluation-student-name">
          {{ item.student.lastName }} {{ item.student.firstName }}
        </h3>
        <span v-if="savedStudentId === item.student.id" class="text-xs font-medium text-green-600">
          Salvat
        </span>
        <span
          v-else-if="item.evaluation !== null && item.evaluation !== undefined"
          class="text-xs text-gray-400"
        >
          Actualizat
        </span>
      </div>

      <!-- Editing mode -->
      <div v-if="editingStudentId === item.student.id">
        <label :for="'eval-' + item.student.id" class="sr-only">
          Evaluare descriptivă pentru {{ item.student.lastName }} {{ item.student.firstName }}
        </label>
        <textarea
          :id="'eval-' + item.student.id"
          v-model="editContent"
          data-testid="evaluation-textarea"
          class="w-full rounded-md border border-gray-300 p-3 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500"
          rows="4"
          placeholder="Scrieți evaluarea descriptivă pentru acest elev..."
        />
        <div class="mt-2 flex gap-2">
          <button
            data-testid="evaluation-save-btn"
            type="button"
            :disabled="isSaving || editContent.trim() === ''"
            class="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white shadow-sm hover:bg-blue-700 disabled:opacity-50"
            @click="save(item.student.id)"
          >
            {{ isSaving ? 'Se salvează...' : 'Salvează' }}
          </button>
          <button
            data-testid="evaluation-cancel-btn"
            type="button"
            class="rounded-md border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
            @click="cancelEdit()"
          >
            Anulează
          </button>
        </div>
      </div>

      <!-- Display mode: show evaluation text or placeholder -->
      <div v-else>
        <p
          v-if="item.evaluation !== null && item.evaluation !== undefined"
          data-testid="evaluation-content"
          class="whitespace-pre-wrap text-sm text-gray-700"
        >
          {{ item.evaluation.content }}
        </p>
        <p v-else class="text-sm italic text-gray-400" data-testid="evaluation-empty">
          Nicio evaluare descriptivă scrisă încă.
        </p>
        <button
          data-testid="evaluation-edit-btn"
          type="button"
          class="mt-2 text-sm font-medium text-blue-600 hover:text-blue-800"
          @click="startEdit(item.student.id, item.evaluation?.content ?? '')"
        >
          {{
            item.evaluation !== null && item.evaluation !== undefined
              ? 'Editează evaluarea'
              : 'Scrie evaluarea'
          }}
        </button>
      </div>
    </div>
  </div>
</template>
