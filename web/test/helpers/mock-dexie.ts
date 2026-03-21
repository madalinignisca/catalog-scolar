/**
 * In-memory mock of the Dexie `db` object exported from `lib/db.ts`.
 *
 * WHY THIS EXISTS
 * ───────────────
 * The real `db` is a Dexie instance backed by IndexedDB.  IndexedDB is a
 * browser API that happy-dom does not fully implement, so importing `lib/db.ts`
 * in a test would either throw or silently fail.
 *
 * Instead, we provide `createMockDb()` — a factory that returns an object with
 * the exact same shape and method signatures as the real Dexie tables, but
 * backed entirely by plain in-memory arrays.  All methods return Promises so
 * that `async/await` code in composables works identically against the mock.
 *
 * WHAT IS MOCKED
 * ──────────────
 * The mock replicates the subset of the Dexie Table API actually used across
 * `lib/sync-queue.ts` and the composables:
 *
 *   table.add(item)
 *   table.get(id)
 *   table.update(id, changes)
 *   table.delete(id)
 *   table.clear()
 *   table.count()
 *   table.where('field').equals(value).toArray()
 *   table.where('field').equals(value).delete()
 *   table.where('field').equals(value).sortBy(key)
 *   table.where('field').anyOf(values).count()
 *
 * USAGE IN A TEST FILE
 * ────────────────────
 *   import { createMockDb } from '../helpers/mock-dexie';
 *
 *   // Create a fresh db mock for this test module:
 *   const mockDb = createMockDb();
 *
 *   // Replace the real db in the module under test:
 *   vi.mock('~/lib/db', () => ({ db: mockDb }));
 *
 *   // Now calls to db.syncQueue.add(...) in sync-queue.ts hit the mock.
 *   it('enqueues a mutation', async () => {
 *     await enqueue('grade', 'create', { numericGrade: 9 });
 *     expect(await mockDb.syncQueue.count()).toBe(1);
 *   });
 *
 * IMPORTANT: call `mockDb.syncQueue.clear()` (or recreate the mock) in
 * beforeEach to avoid state leaking between tests.
 */

import type { CachedGrade, CachedAbsence, SyncMutation, SyncMeta } from '~/lib/db';

// ── Internal helpers ─────────────────────────────────────────────────────────

/**
 * Auto-increment counter used to assign numeric `id` fields to records added
 * to tables whose primary key is `++id` (e.g. syncQueue).
 *
 * Each `createMockDb()` call gets its OWN counter instance so multiple mock
 * databases don't interfere.
 */
function makeCounter(start = 1): { next(): number } {
  let current = start;
  return {
    next() {
      return current++;
    },
  };
}

// ── WhereClause builder ──────────────────────────────────────────────────────

/**
 * Represents the result of calling `table.where('fieldName').equals(v)` or
 * `table.where('fieldName').anyOf(vs)`.
 *
 * Terminal operations available on the result:
 *   .toArray()    → Promise<T[]>
 *   .delete()     → Promise<number>   (returns count of deleted rows)
 *   .sortBy(key)  → Promise<T[]>
 *   .count()      → Promise<number>
 */
interface WhereResult<T> {
  /** Collect all matching records into an array. */
  toArray(): Promise<T[]>;
  /** Delete all matching records. Returns the number of deleted rows. */
  delete(): Promise<number>;
  /**
   * Return all matching records sorted ascending by the given field.
   * Matches the Dexie signature: sortBy(keyPath: string) → Promise<T[]>
   */
  sortBy(keyPath: string): Promise<T[]>;
  /** Count the number of matching records. */
  count(): Promise<number>;
}

/**
 * Represents the result of calling `table.where('fieldName')`.
 * Exposes `.equals()` and `.anyOf()` which return a `WhereResult`.
 */
interface WhereClause<T> {
  /**
   * Match records where `field === value`.
   * @param value - The exact value to match against.
   */
  equals(value: unknown): WhereResult<T>;
  /**
   * Match records where `field` is one of the values in the array.
   * @param values - Array of acceptable values.
   */
  anyOf(values: unknown[]): WhereResult<T>;
}

// ── MockTable ────────────────────────────────────────────────────────────────

/**
 * The public interface of a mock Dexie table.
 *
 * This mirrors the subset of `Dexie.Table<T, TKey>` used in the codebase.
 * The generic parameter `T` is the stored record type (e.g. `SyncMutation`).
 */
export interface MockTable<T extends object> {
  /**
   * Add a record to the table.
   *
   * For tables with an auto-increment primary key (`++id`), the `id` field is
   * automatically assigned if not present.  Returns the new id.
   *
   * @param item - The record to add (id is optional for auto-increment tables).
   * @returns    Promise resolving to the assigned primary key value.
   */
  add(item: Omit<T, 'id'> & { id?: number | string }): Promise<number | string>;

  /**
   * Retrieve a single record by its primary key.
   *
   * @param id - The primary key value to look up.
   * @returns  Promise resolving to the record, or `undefined` if not found.
   */
  get(id: number | string): Promise<T | undefined>;

  /**
   * Merge `changes` into the record identified by `id`.
   *
   * Only the fields present in `changes` are updated; other fields are left
   * untouched.  This mirrors Dexie's `update()` semantics.
   *
   * @param id      - Primary key of the record to update.
   * @param changes - Partial object with fields to overwrite.
   * @returns       Promise resolving to 1 if the record was found and updated,
   *                0 if the id did not exist.
   */
  update(id: number | string, changes: Partial<T>): Promise<number>;

  /**
   * Remove the record with the given primary key.
   *
   * If the id does not exist this is a no-op (no error is thrown).
   *
   * @param id - Primary key of the record to remove.
   */
  delete(id: number | string): Promise<void>;

  /**
   * Remove ALL records from the table, leaving it empty.
   */
  clear(): Promise<void>;

  /**
   * Return the total number of records currently in the table.
   */
  count(): Promise<number>;

  /**
   * Begin a query filtered by a specific field.
   *
   * Call `.equals(value)` or `.anyOf(values)` on the returned object to
   * complete the filter, then chain a terminal operation.
   *
   * @param field - The name of the field to filter on.
   * @returns     A `WhereClause` builder object.
   */
  where(field: string): WhereClause<T>;

  /**
   * Direct access to the underlying in-memory array.
   * Useful in tests for direct inspection without going through async methods.
   *
   * WARNING: Do not mutate this array directly in production-like code; always
   * use the public API methods so that auto-increment counters stay consistent.
   */
  _data: Array<T & { id: number | string }>;
}

// ── Factory ──────────────────────────────────────────────────────────────────

/**
 * A record stored in the mock table.  We intersect T with `{ id: number | string }`
 * to guarantee the id field is always present after an `add()`.
 */
type StoredRecord<T> = T & { id: number | string };

/**
 * Read a named property from an unknown record object without triggering the
 * `security/detect-object-injection` lint rule.
 *
 * The lint rule fires on `obj[key]` when `key` comes from an external source.
 * In our mock, the field names are provided by test authors (trusted), but the
 * rule cannot distinguish that at a static level.  Wrapping the access in this
 * helper with a single `// eslint-disable-line` keeps the disables localised
 * and makes the intent explicit.
 */
function getField(record: Record<string, unknown>, field: string): unknown {
  // eslint-disable-next-line security/detect-object-injection
  return record[field];
}

/**
 * Create one mock table backed by an in-memory array.
 *
 * @param autoIncrementId - Set to `true` for tables whose PK is `++id`
 *                          (Dexie auto-increment).  The mock will assign an
 *                          integer id automatically on `add()`.
 *                          Set to `false` for tables that receive their own id
 *                          (e.g. grades and absences use string UUIDs as PK).
 */
function createMockTable<T extends object>(autoIncrementId: boolean): MockTable<T> {
  /**
   * The in-memory store.  Every record is stored as `StoredRecord<T>`.
   * We keep them in insertion order (standard JS array behaviour).
   */
  const data: Array<StoredRecord<T>> = [];

  /**
   * Auto-increment counter — only used when `autoIncrementId` is true.
   */
  const counter = makeCounter(1);

  /**
   * Build a `WhereResult` for a pre-filtered subset of records.
   * This is a pure helper used by both `.equals()` and `.anyOf()`.
   *
   * @param filtered - The already-filtered records to operate on.
   */
  function buildWhereResult(filtered: Array<StoredRecord<T>>): WhereResult<T> {
    return {
      /**
       * Return a shallow copy of the filtered records as an array.
       * Shallow copy prevents tests from accidentally mutating the store.
       */
      toArray(): Promise<T[]> {
        return Promise.resolve([...filtered]);
      },

      /**
       * Delete every record that matched the filter.
       * Returns the count of deleted records (mirrors Dexie's return value).
       */
      delete(): Promise<number> {
        // Identify the ids of matched records so we can splice them out.
        const idsToDelete = new Set(filtered.map((item) => item.id));
        let count = 0;

        // Iterate backwards so that splicing doesn't shift unprocessed indices.
        for (let i = data.length - 1; i >= 0; i--) {
          // eslint-disable-next-line security/detect-object-injection -- index is a loop counter, not user input
          const item = data[i];
          // `noUncheckedIndexedAccess` means item could be undefined even
          // though we're inside bounds — guard to keep TS happy.
          if (item !== undefined && idsToDelete.has(item.id)) {
            data.splice(i, 1);
            count++;
          }
        }

        return Promise.resolve(count);
      },

      /**
       * Sort the filtered records by the given field name, ascending.
       *
       * Comparison is done with the standard `<` / `>` operators, which
       * handles strings (lexicographic) and numbers (numeric) — the two types
       * we encounter in practice (ISO-8601 date strings sort correctly
       * lexicographically).
       *
       * @param keyPath - The field name to sort by.
       */
      sortBy(keyPath: string): Promise<T[]> {
        const sorted = [...filtered].sort((a, b) => {
          const av = getField(a as Record<string, unknown>, keyPath);
          const bv = getField(b as Record<string, unknown>, keyPath);
          if (av === bv) return 0;
          return (av as string | number) < (bv as string | number) ? -1 : 1;
        });
        return Promise.resolve(sorted);
      },

      /**
       * Return the count of matched records as a number.
       */
      count(): Promise<number> {
        return Promise.resolve(filtered.length);
      },
    };
  }

  // ── The table object itself ────────────────────────────────────────────────

  const table: MockTable<T> = {
    // Expose internal array for test assertions (read-only by convention).
    _data: data,

    add(item: Omit<T, 'id'> & { id?: number | string }): Promise<number | string> {
      let id: number | string;

      if (autoIncrementId) {
        // Auto-increment: ignore any caller-supplied id, assign the next int.
        id = counter.next();
      } else {
        // Caller must supply an id (e.g. a UUID string).
        if (item.id === undefined) {
          return Promise.reject(
            new Error(
              '[mock-dexie] add() called without an id on a non-auto-increment table. ' +
                'Provide an `id` field (e.g. a UUID string).',
            ),
          );
        }
        id = item.id;
      }

      // Merge the id into the record and push it.
      const record = { ...item, id } as StoredRecord<T>;
      data.push(record);
      return Promise.resolve(id);
    },

    get(id: number | string): Promise<T | undefined> {
      // String/number coercion: Dexie normalises numeric-string keys, so we
      // compare both as strings to handle "1" vs 1 transparently.
      const idStr = String(id);
      return Promise.resolve(data.find((item) => String(item.id) === idStr));
    },

    update(id: number | string, changes: Partial<T>): Promise<number> {
      const idStr = String(id);
      const index = data.findIndex((item) => String(item.id) === idStr);
      if (index === -1) {
        // Record not found — return 0 to match Dexie's behaviour.
        return Promise.resolve(0);
      }
      // Merge changes onto the existing record in-place.
      // `data[index]` is guaranteed to exist because findIndex returned >= 0,
      // but with `noUncheckedIndexedAccess` we must satisfy the compiler.
      // eslint-disable-next-line security/detect-object-injection -- index comes from findIndex, not user input
      const existing = data[index];
      if (existing !== undefined) {
        // eslint-disable-next-line security/detect-object-injection -- same as above
        data[index] = { ...existing, ...changes };
      }
      return Promise.resolve(1);
    },

    delete(id: number | string): Promise<void> {
      const idStr = String(id);
      const index = data.findIndex((item) => String(item.id) === idStr);
      if (index !== -1) {
        data.splice(index, 1);
      }
      // No error if not found — matches Dexie behaviour.
      return Promise.resolve();
    },

    clear(): Promise<void> {
      // Truncate the array in-place so that any external references to
      // `_data` also see the empty array (important for test inspection).
      data.splice(0, data.length);
      return Promise.resolve();
    },

    count(): Promise<number> {
      return Promise.resolve(data.length);
    },

    where(field: string): WhereClause<T> {
      /**
       * Return a WhereClause object that captures `field` and, when a
       * comparator is called (.equals / .anyOf), produces a WhereResult.
       */
      return {
        equals(value: unknown): WhereResult<T> {
          // Filter records where `record[field] === value`.
          const filtered = data.filter(
            (item) => getField(item as Record<string, unknown>, field) === value,
          );
          return buildWhereResult(filtered);
        },

        anyOf(values: unknown[]): WhereResult<T> {
          // Filter records where `record[field]` is one of the listed values.
          const valueSet = new Set(values);
          const filtered = data.filter((item) =>
            valueSet.has(getField(item as Record<string, unknown>, field)),
          );
          return buildWhereResult(filtered);
        },
      };
    },
  };

  return table;
}

// ── Public DB mock shape ─────────────────────────────────────────────────────

/**
 * The shape of the object returned by `createMockDb()`.
 *
 * It mirrors the `CatalogDB` class in `lib/db.ts`, exposing the four tables
 * used throughout the application.
 */
export interface MockDb {
  /** Cached grade records (string UUID primary key). */
  grades: MockTable<CachedGrade>;
  /** Cached absence records (string UUID primary key). */
  absences: MockTable<CachedAbsence>;
  /**
   * Offline sync queue entries (auto-increment integer primary key `++id`).
   * Used extensively by `lib/sync-queue.ts`.
   */
  syncQueue: MockTable<SyncMutation>;
  /** Key/value metadata for sync state (string primary key). */
  syncMeta: MockTable<SyncMeta>;
}

/**
 * Create a fresh in-memory mock of the entire CatalogRO Dexie database.
 *
 * Each call returns a completely independent instance — there is no shared
 * state between instances.  Create one mock per test module (or per test if
 * you need complete isolation) and clear individual tables in `beforeEach`.
 *
 * @returns A `MockDb` object whose tables behave like Dexie tables but run
 *          in memory with no IndexedDB dependency.
 *
 * Example:
 *   const mockDb = createMockDb();
 *   vi.mock('~/lib/db', () => ({ db: mockDb }));
 *
 *   beforeEach(() => mockDb.syncQueue.clear());
 *
 *   it('adds to sync queue', async () => {
 *     await enqueue('grade', 'create', { numericGrade: 9 });
 *     expect(await mockDb.syncQueue.count()).toBe(1);
 *   });
 */
export function createMockDb(): MockDb {
  return {
    /**
     * `grades` table — primary key is a string UUID supplied by the caller
     * (NOT auto-increment).  Matches the Dexie schema: `grades: 'id, ...'`.
     */
    grades: createMockTable<CachedGrade>(false),

    /**
     * `absences` table — same pattern as grades, string UUID primary key.
     */
    absences: createMockTable<CachedAbsence>(false),

    /**
     * `syncQueue` table — auto-increment integer primary key `++id`.
     * Matches the Dexie schema: `syncQueue: '++id, status, entityType, createdAt'`.
     *
     * The `id` field on `SyncMutation` is `number | undefined`; after `add()`
     * it will always be a number assigned by the mock counter.
     */
    syncQueue: createMockTable<SyncMutation>(true),

    /**
     * `syncMeta` table — string primary key (`key`).
     * Used to store lightweight key/value pairs like the last-sync timestamp.
     */
    syncMeta: createMockTable<SyncMeta>(false),
  };
}
