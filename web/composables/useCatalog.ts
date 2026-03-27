/**
 * useCatalog — Composable for catalog data management (grades, classes, subjects).
 *
 * This is the main data layer for the teacher's catalog workflow:
 * 1. Fetch the teacher's assigned classes
 * 2. Fetch grades for a specific class + subject + semester
 * 3. Add, update, or delete grades (with offline sync support)
 *
 * All write operations (add/update/delete grade) go through the offline sync
 * queue when the device is offline. When online, they hit the API directly
 * AND enqueue for sync as a safety net. The sync engine handles deduplication
 * via the `client_id` field.
 *
 * Romanian domain terms used in this file:
 * - notă = grade (a mark given to a student)
 * - calificativ = qualifier (FB/B/S/I, used in primary school instead of numbers)
 * - materie = subject (e.g. Matematică, Limba Română)
 * - elev = student
 * - medie = average (computed from grades)
 * - semestru = semester (I or II)
 */

import { api } from '~/lib/api';

// ── Types ──────────────────────────────────────────────────────────────────

/** Education level determines which grading system is used */
export type EducationLevel = 'primary' | 'middle' | 'high';

/** Semester identifier — Romanian school year has two semesters */
export type Semester = 'I' | 'II';

/** Qualifier grades used in primary school (P through class IV) */
export type QualifierGrade = 'FB' | 'B' | 'S' | 'I';

/**
 * Represents a single class assigned to the current teacher.
 * Returned by GET /classes (filtered to teacher's assignments).
 */
export interface TeacherClass {
  /** Unique class identifier (UUID) */
  id: string;
  /** Display name of the class, e.g. "5A", "9B", "P" (pregătitoare) */
  name: string;
  /** Education level determines grading rules (qualifiers vs numeric) */
  educationLevel: EducationLevel;
  /** Numeric grade level: 0=P, 1-4=primary, 5-8=middle, 9-12=high */
  gradeNumber: number;
  /** Number of students currently enrolled in this class */
  studentCount: number;
  /** The subjects this teacher teaches in this class */
  subjects: TeacherSubject[];
  /** ID of the homeroom teacher (diriginte) for this class */
  homeroomTeacherId: string | null;
}

/**
 * A subject the teacher is assigned to teach in a specific class.
 * Comes from the class_subject_teachers join table.
 */
export interface TeacherSubject {
  /** Unique subject identifier (UUID) */
  id: string;
  /** Full subject name, e.g. "Matematică" */
  name: string;
  /** Short abbreviation, e.g. "MAT" */
  shortName: string | null;
  /** Whether this subject has a thesis exam (teză) */
  hasThesis: boolean;
}

/**
 * A student enrolled in a class, with their grades for a specific subject.
 * This is the main data structure for the grade grid.
 */
export interface StudentWithGrades {
  /** Student's unique identifier (UUID) */
  studentId: string;
  /** Student's first name */
  firstName: string;
  /** Student's last name */
  lastName: string;
  /** All grades this student has for the selected subject + semester */
  grades: Grade[];
  /** Computed average for this subject + semester (null if not enough grades) */
  average: number | null;
  /** Final qualifier for primary level (null for middle/high) */
  qualifierFinal: QualifierGrade | null;
}

/**
 * A single grade entry in the catalog.
 * Can be either a numeric grade (1-10) or a qualifier (FB/B/S/I).
 */
export interface Grade {
  /** Server-assigned grade ID (UUID). May be a client_id if not yet synced. */
  id: string;
  /** Numeric grade value (1-10), null if this is a qualifier grade */
  numericGrade: number | null;
  /** Qualifier value (FB/B/S/I), null if this is a numeric grade */
  qualifierGrade: QualifierGrade | null;
  /** Whether this grade is the semester thesis (teză) */
  isThesis: boolean;
  /** Date when the grade was given (ISO date string, e.g. "2026-10-15") */
  gradeDate: string;
  /** Optional description/observation about the grade */
  description: string | null;
  /** ID of the teacher who gave this grade */
  teacherId: string;
  /** When this grade was last updated on the server */
  updatedAt: string;
}

/**
 * Data required to create a new grade.
 * Sent to POST /catalog/grades.
 */
export interface CreateGradePayload {
  /** Which student receives this grade */
  studentId: string;
  /** Which class the student belongs to */
  classId: string;
  /** Which subject this grade is for */
  subjectId: string;
  /** Which semester (I or II) */
  semester: Semester;
  /** Numeric grade value (1-10), required for middle/high school */
  numericGrade?: number;
  /** Qualifier value (FB/B/S/I), required for primary school */
  qualifierGrade?: QualifierGrade;
  /** Whether this is the semester thesis grade */
  isThesis?: boolean;
  /** Date the grade was given */
  gradeDate: string;
  /** Optional teacher's observation */
  description?: string;
}

/**
 * Data required to update an existing grade.
 * Sent to PUT /catalog/grades/{gradeId}.
 */
export interface UpdateGradePayload {
  /** New numeric grade value (1-10) */
  numericGrade?: number;
  /** New qualifier value (FB/B/S/I) */
  qualifierGrade?: QualifierGrade;
  /** Updated date */
  gradeDate?: string;
  /** Updated description */
  description?: string;
}

// ── API Response Types ─────────────────────────────────────────────────────

/** Response type for GET /classes (api() auto-unwraps the { data: ... } envelope) */
type ClassesResponse = TeacherClass[];

/**
 * Response type for class teachers (GET /classes/{classId}/teachers).
 * The API returns a FLAT list of teacher-subject assignments (one row per
 * teacher-subject pair), not grouped by teacher. After snakeToCamel conversion:
 *   { teacherId, firstName, lastName, subjectId, subjectName, hoursPerWeek }
 */
type ClassTeachersResponse = Array<{
  teacherId: string;
  firstName: string;
  lastName: string;
  subjectId: string;
  subjectName: string;
  hoursPerWeek: number;
}>;

/**
 * Response type for grades grid (GET /catalog/classes/{classId}/subjects/{subjectId}/grades).
 * The API returns a nested structure where each entry has a `student` object
 * and a `grades` array. After snakeToCamel conversion:
 *   { students: [{ student: { id, firstName, lastName }, grades: [...] }] }
 * We flatten this in fetchClassGrades() to match StudentWithGrades.
 */
interface GradesResponse {
  students: Array<{
    student: { id: string; firstName: string; lastName: string };
    grades: Grade[];
  }>;
}

/** Response type for single grade operations (POST/PUT) */
type GradeResponse = Grade;

// ── Composable ─────────────────────────────────────────────────────────────

/**
 * Main composable for catalog data — classes, grades, and grade CRUD.
 *
 * Usage in a component:
 * ```ts
 * const { classes, fetchClasses, fetchClassGrades, addGrade } = useCatalog();
 * await fetchClasses(); // loads teacher's classes
 * ```
 */
export function useCatalog() {
  /** List of classes assigned to the current teacher */
  const classes = ref<TeacherClass[]>([]);

  /** True while any catalog data is being loaded */
  const isLoading = ref(false);

  /** Stores the last error message from any operation */
  const error = ref<string | null>(null);

  /** The currently loaded grade grid (students + their grades) */
  const gradeGrid = ref<StudentWithGrades[]>([]);

  /** Get access to the offline sync queue for write operations */
  const { enqueueMutation, isOnline } = useOfflineSync();

  // ── Read Operations ────────────────────────────────────────────────────

  /**
   * Fetch all classes assigned to the current teacher.
   * Calls GET /classes which returns only classes where the teacher has assignments.
   * Each class includes the subjects the teacher teaches and the student count.
   */
  async function fetchClasses(): Promise<void> {
    isLoading.value = true;
    error.value = null;

    try {
      const response = await api<ClassesResponse>('/classes');
      classes.value = response;
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Nu s-au putut încărca clasele';
      classes.value = [];
    } finally {
      isLoading.value = false;
    }
  }

  /**
   * Fetch subjects that the current teacher teaches in a specific class.
   * Calls GET /classes/{classId}/teachers and filters to the current user.
   *
   * @param classId - The class to look up teacher assignments for
   * @param currentUserId - The current teacher's user ID (to filter results)
   * @returns Array of subjects the teacher teaches in this class
   */
  async function fetchTeacherSubjects(
    classId: string,
    currentUserId: string,
  ): Promise<TeacherSubject[]> {
    try {
      const response = await api<ClassTeachersResponse>(`/classes/${classId}/teachers`);

      /* The API returns a flat list of teacher-subject assignments:
       *   [{ teacherId, subjectId, subjectName, ... }, ...]
       * Filter to the current teacher, then map each row to a TeacherSubject. */
      const myAssignments = response.filter((t) => t.teacherId === currentUserId);

      return myAssignments.map((a) => ({
        id: a.subjectId,
        name: a.subjectName,
        shortName: null,
        hasThesis: false,
      }));
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Nu s-au putut încărca materiile';
      return [];
    }
  }

  /**
   * Fetch the full grade grid for a specific class, subject, and semester.
   * This is the core data for the GradeGrid component — it returns each
   * student with their grades and computed average.
   *
   * @param classId - Which class to fetch grades for
   * @param subjectId - Which subject's grades to fetch
   * @param semester - Which semester ('I' or 'II')
   */
  async function fetchClassGrades(
    classId: string,
    subjectId: string,
    semester: Semester,
  ): Promise<void> {
    isLoading.value = true;
    error.value = null;

    try {
      const response = await api<GradesResponse>(
        `/catalog/classes/${classId}/subjects/${subjectId}/grades?semester=${semester}`,
      );

      // Flatten the nested API response into the StudentWithGrades format.
      // API returns: { student: { id, firstName, lastName }, grades: [...] }
      // We need:    { studentId, firstName, lastName, grades: [...], average, qualifierFinal }
      gradeGrid.value = response.students.map((entry) => ({
        studentId: entry.student.id,
        firstName: entry.student.firstName,
        lastName: entry.student.lastName,
        grades: entry.grades,
        average: computeNumericAverage(entry.grades),
        qualifierFinal: null,
      }));
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Nu s-au putut încărca notele';
      gradeGrid.value = [];
    } finally {
      isLoading.value = false;
    }
  }

  // ── Write Operations (with offline sync support) ───────────────────────

  /**
   * Add a new grade for a student.
   *
   * When ONLINE: sends the grade directly to the API via POST /catalog/grades,
   * then also enqueues to the sync queue for safety (dedup by client_id).
   *
   * When OFFLINE: only enqueues the mutation to IndexedDB. The sync engine
   * will flush it when connectivity returns.
   *
   * @param payload - Grade data (student, subject, value, date, etc.)
   * @returns The created grade (from server if online, optimistic if offline)
   */
  async function addGrade(payload: CreateGradePayload): Promise<Grade | null> {
    error.value = null;

    /* Generate a client-side ID for deduplication.
     * This ID is sent to the server and used to prevent duplicate grades
     * if the same mutation is synced more than once. */
    const clientId = crypto.randomUUID();
    const clientTimestamp = new Date().toISOString();

    /* Build the API request body matching the POST /catalog/grades spec */
    const body = {
      student_id: payload.studentId,
      class_id: payload.classId,
      subject_id: payload.subjectId,
      semester: payload.semester,
      numeric_grade: payload.numericGrade ?? null,
      qualifier_grade: payload.qualifierGrade ?? null,
      is_thesis: payload.isThesis ?? false,
      grade_date: payload.gradeDate,
      description: payload.description ?? null,
      client_id: clientId,
      client_timestamp: clientTimestamp,
    };

    try {
      if (isOnline.value) {
        /* Online path: send directly to the server */
        const response = await api<GradeResponse>('/catalog/grades', {
          method: 'POST',
          body,
        });

        /* Also enqueue for sync safety — the server deduplicates by client_id */
        await enqueueMutation('grade', 'create', body);

        return response;
      } else {
        /* Offline path: enqueue the mutation for later sync */
        await enqueueMutation('grade', 'create', body);

        /* Return an optimistic grade object so the UI can update immediately.
         * The real server ID will be assigned once the sync engine processes it. */
        return {
          id: clientId,
          numericGrade: payload.numericGrade ?? null,
          qualifierGrade: payload.qualifierGrade ?? null,
          isThesis: payload.isThesis ?? false,
          gradeDate: payload.gradeDate,
          description: payload.description ?? null,
          teacherId: '', // Will be set by server from JWT
          updatedAt: clientTimestamp,
        };
      }
    } catch (e: unknown) {
      /* If the direct API call fails, fall back to offline enqueue */
      error.value = e instanceof Error ? e.message : 'Nu s-a putut adăuga nota';
      await enqueueMutation('grade', 'create', body);
      return null;
    }
  }

  /**
   * Update an existing grade.
   *
   * @param gradeId - The ID of the grade to update
   * @param payload - The fields to update
   * @returns The updated grade from the server, or null on error
   */
  async function updateGrade(gradeId: string, payload: UpdateGradePayload): Promise<Grade | null> {
    error.value = null;

    const clientTimestamp = new Date().toISOString();

    /* Build the API request body matching PUT /catalog/grades/{gradeId} */
    const body = {
      numeric_grade: payload.numericGrade ?? null,
      qualifier_grade: payload.qualifierGrade ?? null,
      grade_date: payload.gradeDate ?? null,
      description: payload.description ?? null,
      client_timestamp: clientTimestamp,
    };

    try {
      if (isOnline.value) {
        /* Online: update directly on server */
        const response = await api<GradeResponse>(`/catalog/grades/${gradeId}`, {
          method: 'PUT',
          body,
        });

        /* Enqueue for sync safety */
        await enqueueMutation('grade', 'update', {
          ...body,
          grade_id: gradeId,
        });

        return response;
      } else {
        /* Offline: enqueue for later */
        await enqueueMutation('grade', 'update', {
          ...body,
          grade_id: gradeId,
        });
        return null;
      }
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Nu s-a putut modifica nota';
      await enqueueMutation('grade', 'update', {
        ...body,
        grade_id: gradeId,
      });
      return null;
    }
  }

  /**
   * Delete a grade (soft delete — sets deleted_at on the server).
   *
   * @param gradeId - The ID of the grade to delete
   * @returns True if the delete was successful (or enqueued for offline)
   */
  async function deleteGrade(gradeId: string): Promise<boolean> {
    error.value = null;

    const clientTimestamp = new Date().toISOString();

    try {
      if (isOnline.value) {
        /* Online: delete directly on server */
        await api(`/catalog/grades/${gradeId}`, { method: 'DELETE' });

        /* Enqueue for sync safety */
        await enqueueMutation('grade', 'delete', {
          grade_id: gradeId,
          client_timestamp: clientTimestamp,
        });

        return true;
      } else {
        /* Offline: enqueue for later */
        await enqueueMutation('grade', 'delete', {
          grade_id: gradeId,
          client_timestamp: clientTimestamp,
        });
        return true;
      }
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Nu s-a putut șterge nota';
      await enqueueMutation('grade', 'delete', {
        grade_id: gradeId,
        client_timestamp: clientTimestamp,
      });
      return false;
    }
  }

  // ── Helpers ────────────────────────────────────────────────────────────

  /**
   * Optimistically add a grade to the local grid without refetching.
   * Called after addGrade() succeeds to update the UI immediately.
   *
   * @param studentId - Which student to add the grade to
   * @param grade - The grade object to insert into the grid
   */
  function addGradeToGrid(studentId: string, grade: Grade): void {
    const student = gradeGrid.value.find((s) => s.studentId === studentId);
    if (student) {
      student.grades.push(grade);
      /* Recalculate the average after adding the new grade */
      student.average = computeNumericAverage(student.grades);
    }
  }

  /**
   * Optimistically update a grade in the local grid.
   *
   * @param gradeId - Which grade to update
   * @param updates - The fields to change
   */
  function updateGradeInGrid(gradeId: string, updates: Partial<Grade>): void {
    for (const student of gradeGrid.value) {
      const gradeIndex = student.grades.findIndex((g) => g.id === gradeId);
      if (gradeIndex !== -1) {
        student.grades[gradeIndex] = {
          ...student.grades[gradeIndex],
          ...updates,
        };
        /* Recalculate the average after modifying the grade */
        student.average = computeNumericAverage(student.grades);
        break;
      }
    }
  }

  /**
   * Optimistically remove a grade from the local grid.
   *
   * @param gradeId - Which grade to remove
   */
  function removeGradeFromGrid(gradeId: string): void {
    for (const student of gradeGrid.value) {
      const gradeIndex = student.grades.findIndex((g) => g.id === gradeId);
      if (gradeIndex !== -1) {
        student.grades.splice(gradeIndex, 1);
        /* Recalculate the average after removing the grade */
        student.average = computeNumericAverage(student.grades);
        break;
      }
    }
  }

  /**
   * Compute the arithmetic average of numeric grades for a student.
   * Returns null if there are no numeric grades (e.g. primary level uses qualifiers).
   *
   * Note: This is a simplified client-side calculation. The server computes
   * the official average (including thesis weighting and rounding rules).
   *
   * @param grades - Array of grade objects
   * @returns The arithmetic average rounded to 2 decimals, or null
   */
  function computeNumericAverage(grades: Grade[]): number | null {
    const numericGrades = grades
      .filter((g) => g.numericGrade != null)
      .map((g) => g.numericGrade as number);

    if (numericGrades.length === 0) return null;

    const sum = numericGrades.reduce((acc, val) => acc + val, 0);
    return Math.round((sum / numericGrades.length) * 100) / 100;
  }

  return {
    /** Reactive list of teacher's classes */
    classes: readonly(classes),
    /** True while loading data */
    isLoading: readonly(isLoading),
    /** Last error message, null if no error */
    error: readonly(error),
    /** Reactive grade grid (students + grades) for the current view */
    gradeGrid,
    /** Fetch teacher's assigned classes */
    fetchClasses,
    /** Fetch subjects for a specific class (filtered to current teacher) */
    fetchTeacherSubjects,
    /** Fetch grade grid for a class + subject + semester */
    fetchClassGrades,
    /** Add a new grade (online or offline) */
    addGrade,
    /** Update an existing grade */
    updateGrade,
    /** Delete a grade (soft delete) */
    deleteGrade,
    /** Optimistically add a grade to the local grid */
    addGradeToGrid,
    /** Optimistically update a grade in the local grid */
    updateGradeInGrid,
    /** Optimistically remove a grade from the local grid */
    removeGradeFromGrid,
  };
}
