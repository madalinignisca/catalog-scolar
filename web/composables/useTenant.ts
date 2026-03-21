/**
 * useTenant — Composable for managing school (tenant) context.
 *
 * CatalogRO is a multi-tenant app — every teacher, student, and admin
 * belongs to a specific school. This composable fetches and stores the
 * current school's information so that the layout, dashboard, and other
 * components can display the school name and configuration.
 *
 * The school is resolved server-side from the JWT token's `school_id` claim,
 * so the client simply calls GET /schools/current to get the details.
 *
 * State is shared across all components that call `useTenant()` because
 * the reactive refs are declared at module level (outside the function).
 */

import { api } from '~/lib/api';

// ── Types ──────────────────────────────────────────────────────────────────

/**
 * Represents the school the current user belongs to.
 * Maps to the `schools` table in the database.
 */
export interface School {
  /** Unique school identifier (UUID v7) */
  id: string;
  /** Full school name, e.g. "Școala Gimnazială Nr. 25" */
  name: string;
  /** District (ISJ) identifier this school belongs to */
  districtId: string;
  /** SIIIR code for interoperability with the Ministry of Education */
  siiirCode: string | null;
  /**
   * Education levels offered by this school.
   * Determines which evaluation rules apply (qualifiers vs numeric grades).
   * - 'primary' = clasele P-IV (calificative FB/B/S/I)
   * - 'middle' = clasele V-VIII (note 1-10)
   * - 'high' = clasele IX-XII (note 1-10, teze)
   */
  educationLevels: Array<'primary' | 'middle' | 'high'>;
  /** Physical address of the school */
  address: string | null;
  /** City where the school is located */
  city: string | null;
  /** County (județ) — e.g. "Cluj", "București" */
  county: string | null;
  /** Contact phone number */
  phone: string | null;
  /** Contact email address */
  email: string | null;
}

/**
 * Shape of the API response from GET /schools/current.
 * Follows the standard CatalogRO envelope: { data: ... }
 */
interface SchoolResponse {
  data: School;
}

// ── Shared reactive state ──────────────────────────────────────────────────
// These refs live at module level so all components share the same school data.
// This is the standard Nuxt 3 pattern for global singleton composables.

/** The currently loaded school. Null if not yet fetched or if fetch failed. */
const currentSchool = ref<School | null>(null);

/** True while the school data is being fetched from the API */
const isLoading = ref(false);

/** Stores the last error message if the fetch failed */
const error = ref<string | null>(null);

// ── Composable ─────────────────────────────────────────────────────────────

/**
 * Provides reactive access to the current school (tenant) context.
 *
 * Usage:
 * ```ts
 * const { currentSchool, fetchCurrentSchool } = useTenant();
 * await fetchCurrentSchool(); // call once on app load
 * console.log(currentSchool.value?.name); // "Școala Gimnazială Nr. 25"
 * ```
 */
export function useTenant() {
  /**
   * Fetches the current school's details from the API.
   * Called once on app initialization (in the default layout).
   * The server determines which school to return based on the JWT's school_id claim.
   */
  async function fetchCurrentSchool(): Promise<void> {
    isLoading.value = true;
    error.value = null;

    try {
      const response = await api<SchoolResponse>('/schools/current');
      currentSchool.value = response.data;
    } catch (e: unknown) {
      /* If the fetch fails (e.g. network error, token expired),
       * store the error message so the UI can display a warning */
      error.value = e instanceof Error ? e.message : 'Nu s-au putut încărca datele școlii';
      currentSchool.value = null;
    } finally {
      isLoading.value = false;
    }
  }

  return {
    /** Reactive reference to the current school. Read-only to prevent accidental mutations. */
    currentSchool: readonly(currentSchool),
    /** True while fetching school data */
    isLoading: readonly(isLoading),
    /** Error message if the last fetch failed, null otherwise */
    error: readonly(error),
    /** Call this to load/reload the current school from the API */
    fetchCurrentSchool,
  };
}
