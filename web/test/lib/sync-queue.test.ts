/**
 * Unit tests for lib/sync-queue.ts
 *
 * WHAT THIS FILE TESTS
 * ────────────────────
 * The sync queue is the heart of the offline-first architecture.  When the
 * app is offline (or the server is unreachable), every grade/absence mutation
 * is written into IndexedDB via `enqueue()` instead of being sent immediately
 * to the API.  A background process later reads the queue, sends the mutations
 * one by one, and updates each record's status accordingly.
 *
 * This file checks all the lifecycle transitions:
 *   pending → (enqueue)      → pending
 *   pending → (markSyncing)  → syncing
 *   syncing → (markSynced)   → [deleted]
 *   syncing → (markFailed)   → pending  (reset for retry)
 *
 * HOW MOCKING WORKS HERE
 * ──────────────────────
 * `sync-queue.ts` imports `db` from `~/lib/db`.  That module creates a real
 * Dexie instance backed by IndexedDB — a browser API that the happy-dom
 * environment does not fully implement.
 *
 * We intercept the import using `vi.mock()` with an ASYNC factory.  The async
 * factory is allowed to use `await import(...)`, which goes through Vite's
 * module transformer just like a normal ES import, so TypeScript files (like
 * our mock-dexie helper) resolve correctly.
 *
 * The factory writes the freshly created mock into `mockDbHolder.db` — a
 * plain object declared at module scope.  Because the factory runs before any
 * test code, `mockDbHolder.db` is populated by the time `beforeEach` and the
 * test bodies run.
 *
 * STATE ISOLATION
 * ───────────────
 * `beforeEach` calls `mockDbHolder.db.syncQueue.clear()` to wipe all rows
 * before every test.  This prevents one test's data from leaking into the
 * next — a common source of flaky tests.
 */

/// <reference types="vitest/globals" />

// import-x/order is disabled for this file because Vitest's vi.hoisted() + vi.mock() pattern
// requires non-import code to appear between two import blocks (the type-only imports above
// and the module-under-test imports below the mock setup). This is a structural requirement
// of the Vitest mock system, not a style choice.
/* eslint-disable import-x/order */

import type { MockDb } from '../helpers/mock-dexie';
import type { SyncMutation } from '~/lib/db';

// ── Shared mock holder ────────────────────────────────────────────────────────

/**
 * WHY vi.hoisted() IS REQUIRED
 * ─────────────────────────────
 * Vitest physically lifts every `vi.mock(...)` call — and the code they close
 * over — above ALL other statements, including `import` declarations and
 * `const` / `let` bindings.  Any variable that is simply declared at the top
 * of the file is in the "temporal dead zone" (TDZ) when the mock factory runs,
 * causing a ReferenceError.
 *
 * `vi.hoisted()` is the one escape hatch: its callback is also hoisted to the
 * same level as `vi.mock`, so bindings created inside it are initialised
 * before any mock factory executes.  We use this to create a holder object
 * (`mockDbHolder`) that the async `vi.mock` factory can write to, and that
 * the test bodies can read from later.
 *
 * The holder is a plain `{ db: null }` object so the factory can set its
 * `.db` property (mutating a property is fine; reassigning a `const` would
 * not be).
 */
const mockDbHolder = vi.hoisted<{ db: MockDb | null }>(() => ({ db: null }));

// ── Module mock ───────────────────────────────────────────────────────────────

/**
 * Replace the real `~/lib/db` module with our in-memory mock.
 *
 * The factory is ASYNC so it can use `await import(...)`.  This is important:
 * Vite compiles `.ts` files on demand; a synchronous `require()` would bypass
 * that pipeline and fail to resolve TypeScript sources.  An async factory
 * goes through the normal Vite transform path.
 *
 * Steps:
 * 1. Import `createMockDb` from the helper (fully compiled by Vite, no CJS).
 * 2. Create one mock DB instance for this entire test module.
 * 3. Store it in `mockDbHolder.db` so the test bodies can access it.
 * 4. Return `{ db: mockDb }` — the shape that `sync-queue.ts` destructures.
 */
vi.mock('~/lib/db', async () => {
  const { createMockDb } = await import('../helpers/mock-dexie');
  const mockDb = createMockDb();
  mockDbHolder.db = mockDb;
  return { db: mockDb };
});

// ── Import module under test ─────────────────────────────────────────────────

/**
 * These imports must come AFTER the `vi.mock()` declaration in source order.
 * At runtime Vitest's hoisting mechanism ensures the mock is registered before
 * any module is loaded, but placing imports here makes the intent explicit.
 */
import {
  enqueue,
  getPending,
  markSyncing,
  markSynced,
  markFailed,
  pendingCount,
  clearCompleted,
  isExhausted,
} from '~/lib/sync-queue';

// ── Convenience accessor ──────────────────────────────────────────────────────

/**
 * Return the mock DB, asserting it was initialised by the `vi.mock` factory.
 *
 * We use a getter function (rather than accessing `mockDbHolder.db` directly)
 * so TypeScript narrows the type to `MockDb` (non-null) for us, keeping test
 * bodies free of `!` non-null assertions.
 */
function db(): MockDb {
  if (!mockDbHolder.db) {
    throw new Error('[test] mockDbHolder.db is null — vi.mock factory did not run');
  }
  return mockDbHolder.db;
}

// ── State isolation ───────────────────────────────────────────────────────────

/**
 * Wipe the sync queue before every test.
 *
 * Without this, a row added in test 1 would still be present in test 2,
 * causing counts, status checks, and array comparisons to fail spuriously.
 */
beforeEach(async () => {
  await db().syncQueue.clear();
});

// ── Test suite ────────────────────────────────────────────────────────────────

describe('sync-queue', () => {
  // ── Test 1 ─────────────────────────────────────────────────────────────────

  it('enqueue() adds mutation to pending queue', async () => {
    /**
     * WHY THIS TEST EXISTS
     * Verify that calling `enqueue()` writes a record into the DB and that the
     * record arrives with `status: 'pending'` — the initial lifecycle state
     * that the background sync worker looks for.
     *
     * WHAT WE ASSERT
     * 1. The syncQueue table has exactly 1 row after enqueue.
     * 2. That row's `status` is 'pending'.
     */

    // Act — add one grade-create mutation to the queue.
    await enqueue('grade', 'create', { numericGrade: 8, studentId: 'stu-1' });

    // Assert — the mock DB should now hold exactly one row.
    const count = await db().syncQueue.count();
    expect(count).toBe(1);

    // Retrieve the stored row directly from the mock's internal array.
    // `_data` is a synchronous array exposed by the mock for test inspection.
    const [storedItem] = db().syncQueue._data;

    // The item must have status 'pending' — it hasn't been picked up yet.
    expect(storedItem?.status).toBe('pending');
  });

  // ── Test 2 ─────────────────────────────────────────────────────────────────

  it('getPending() returns only pending mutations', async () => {
    /**
     * WHY THIS TEST EXISTS
     * `getPending()` is called by the sync worker to decide which mutations
     * need to be sent to the server.  It must return ONLY rows with
     * `status === 'pending'`, not rows that are already in-flight ('syncing')
     * or errored ('failed').
     *
     * SETUP
     * We bypass `enqueue()` and insert rows directly into the mock so we can
     * control the exact status values without going through the public API.
     * This tests the filter logic in isolation from the enqueue logic.
     */

    // Shared timestamp — ISO-8601 strings sort lexicographically, which is
    // why the DB schema indexes `createdAt`.
    const now = new Date().toISOString();

    // Minimal SyncMutation base — we spread this for each row and override
    // only the fields that differ between rows.
    const base: Omit<SyncMutation, 'id' | 'status'> = {
      clientId: 'client-base',
      entityType: 'grade',
      action: 'create',
      data: { numericGrade: 7 },
      clientTimestamp: now,
      attempts: 0,
      createdAt: now,
    };

    // Insert one row for each possible status value.
    await db().syncQueue.add({ ...base, clientId: 'c1', status: 'pending' });
    await db().syncQueue.add({ ...base, clientId: 'c2', status: 'syncing' });
    await db().syncQueue.add({ ...base, clientId: 'c3', status: 'failed' });

    // Call the function under test.
    const pending = await getPending();

    // Only the 'pending' row should be returned.
    expect(pending).toHaveLength(1);
    expect(pending[0]?.status).toBe('pending');
    expect(pending[0]?.clientId).toBe('c1');
  });

  // ── Test 3 ─────────────────────────────────────────────────────────────────

  it('markSyncing() updates status', async () => {
    /**
     * WHY THIS TEST EXISTS
     * When the background sync worker picks up a mutation to send to the
     * server, it calls `markSyncing(id)` to mark the row as in-flight.  This
     * prevents the worker from picking up the same row again if it restarts
     * before the response arrives.
     *
     * WHAT WE ASSERT
     * After calling markSyncing, the row's status must be 'syncing'.
     */

    // Arrange — add a mutation and capture the clientId returned by enqueue.
    const clientId = await enqueue('absence', 'create', { absenceDate: '2026-01-10' });

    // Find the row in the mock's internal array to get its numeric auto-id.
    // The `_data` array contains StoredRecord objects with an `id` field
    // auto-assigned by the mock's counter.
    const row = db().syncQueue._data.find((r) => r.clientId === clientId);
    expect(row).toBeDefined(); // Guard: confirm enqueue actually stored the row.

    // Convert the auto-assigned id to a number.  The mock stores ids as
    // `number | string`; Number() coerces safely without a type assertion.
    const rowId = (row ?? { id: 0 }).id;

    // Act — transition the mutation to 'syncing'.
    await markSyncing(rowId);

    // Assert — read the updated row back and check its status.
    const updated = await db().syncQueue.get(rowId);
    expect(updated?.status).toBe('syncing');
  });

  // ── Test 4 ─────────────────────────────────────────────────────────────────

  it('markSynced() deletes the mutation from the queue', async () => {
    /**
     * WHY THIS TEST EXISTS
     * Once the server has acknowledged a mutation, the local copy in IndexedDB
     * is no longer needed.  `markSynced(id)` should remove the row entirely so
     * the queue doesn't grow indefinitely over time.
     *
     * WHAT WE ASSERT
     * After calling markSynced, the row must not exist in the DB (count === 0).
     */

    // Arrange — enqueue a mutation and locate its auto-assigned id.
    const clientId = await enqueue('grade', 'update', { numericGrade: 9 });
    const row = db().syncQueue._data.find((r) => r.clientId === clientId);
    expect(row).toBeDefined();

    const rowId = (row ?? { id: 0 }).id;

    // Confirm the row exists before we delete it.
    expect(await db().syncQueue.count()).toBe(1);

    // Act — mark as synced (this should delete the row from the queue).
    await markSynced(rowId);

    // Assert — the table should now be empty.
    expect(await db().syncQueue.count()).toBe(0);

    // Double-check: get() should return undefined for the deleted id.
    const deleted = await db().syncQueue.get(rowId);
    expect(deleted).toBeUndefined();
  });

  // ── Test 5 ─────────────────────────────────────────────────────────────────

  it('markFailed() resets status to pending, stores error, increments retry count', async () => {
    /**
     * WHY THIS TEST EXISTS
     * When a network request fails (e.g. 503 from the server), the worker
     * calls `markFailed(id, errorMessage)`.  The mutation should:
     *   - be reset to 'pending' so it will be retried on the next flush cycle
     *   - have the error message stored in `lastError` for debugging
     *   - have its `attempts` counter incremented so we can detect exhaustion
     *
     * Note: `markFailed` takes (id: number, error: string) — a string, not an
     * Error object.  It resets status to 'pending' (not 'failed') so the row
     * re-enters the retry loop automatically.
     */

    // Arrange — enqueue a fresh mutation (attempts starts at 0).
    const clientId = await enqueue('grade', 'delete', { gradeId: 'g-42' });
    const row = db().syncQueue._data.find((r) => r.clientId === clientId);
    expect(row).toBeDefined();

    const rowId = (row ?? { id: 0 }).id;

    // Act — simulate a failure with a descriptive error message.
    await markFailed(rowId, 'network error');

    // Assert — re-read the updated row.
    const updated = await db().syncQueue.get(rowId);

    // Status must be reset to 'pending' (not 'failed') so the row is retried.
    expect(updated?.status).toBe('pending');

    // The error message should be stored for visibility in logs/admin UI.
    expect(updated?.lastError).toBe('network error');

    // Attempts was 0 before; after one failure it should be incremented to 1.
    expect(updated?.attempts).toBe(1);
  });

  // ── Test 6 ─────────────────────────────────────────────────────────────────

  it('pendingCount() returns count of pending and syncing mutations', async () => {
    /**
     * WHY THIS TEST EXISTS
     * `pendingCount()` is used by UI badges (e.g. "3 items waiting to sync")
     * and by the sync worker to decide whether to start a flush cycle.
     * It must count BOTH 'pending' AND 'syncing' rows — items that are queued
     * or currently being processed — but NOT 'failed' rows.
     *
     * SETUP
     * We insert rows directly so we can control exact status values.
     * Distribution: 2 pending + 1 syncing + 1 failed → expected count of 3.
     */

    const now = new Date().toISOString();

    const base: Omit<SyncMutation, 'id' | 'status'> = {
      clientId: 'client-x',
      entityType: 'absence',
      action: 'create',
      data: { absenceDate: '2026-02-01' },
      clientTimestamp: now,
      attempts: 0,
      createdAt: now,
    };

    // Insert 2 pending rows.
    await db().syncQueue.add({ ...base, clientId: 'p1', status: 'pending' });
    await db().syncQueue.add({ ...base, clientId: 'p2', status: 'pending' });

    // Insert 1 syncing row (currently in-flight to the server).
    await db().syncQueue.add({ ...base, clientId: 's1', status: 'syncing' });

    // Insert 1 failed row (should NOT contribute to the count).
    await db().syncQueue.add({ ...base, clientId: 'f1', status: 'failed' });

    // Act — get the count.
    const count = await pendingCount();

    // Assert — pending (2) + syncing (1) = 3; failed (1) excluded.
    expect(count).toBe(3);
  });

  // ── Test 7 ─────────────────────────────────────────────────────────────────

  it('isExhausted() returns true at MAX_RETRIES', () => {
    /**
     * WHY THIS TEST EXISTS
     * `isExhausted()` guards the retry loop: when a mutation has been retried
     * MAX_RETRIES times (5), the worker stops retrying it and surfaces a
     * permanent failure alert to the teacher or admin.
     *
     * MAX_RETRIES = 5 (defined as a module constant in sync-queue.ts).
     *
     * WHAT WE ASSERT
     * - attempts === 5 → exhausted (true)
     * - attempts === 4 → NOT exhausted (false)
     *
     * `isExhausted()` is a pure synchronous function — it only reads the
     * `attempts` field and performs no DB operation.  We construct inline
     * SyncMutation objects rather than writing to the DB.
     */

    const now = new Date().toISOString();

    // Base mutation stub — only `attempts` matters for this function.
    const baseMutation: SyncMutation = {
      id: 99,
      clientId: 'exhaustion-test',
      entityType: 'grade',
      action: 'create',
      data: {},
      clientTimestamp: now,
      attempts: 0, // overridden in each case below
      status: 'pending',
      createdAt: now,
    };

    // Case A: attempts === MAX_RETRIES (5) → should be exhausted.
    const exhaustedMutation: SyncMutation = { ...baseMutation, attempts: 5 };
    expect(isExhausted(exhaustedMutation)).toBe(true);

    // Case B: attempts === MAX_RETRIES - 1 (4) → should NOT be exhausted.
    const notYetExhaustedMutation: SyncMutation = { ...baseMutation, attempts: 4 };
    expect(isExhausted(notYetExhaustedMutation)).toBe(false);
  });

  // ── Test 8 ─────────────────────────────────────────────────────────────────

  it('clearCompleted() runs without error on empty result set', async () => {
    /**
     * WHY THIS TEST EXISTS
     * `clearCompleted()` deletes rows with status === 'synced' as a periodic
     * cleanup task.  It must not throw when the queue is empty — this is the
     * common case after startup or after a fresh install with no offline data.
     *
     * Note: the `SyncMutation.status` union is `'pending' | 'syncing' | 'failed'`
     * — there is no 'synced' variant.  `markSynced()` deletes the row
     * immediately instead of transitioning to 'synced'.  `clearCompleted()` is
     * therefore a defensive cleanup; in normal operation it finds nothing to
     * delete, and it must handle that gracefully.
     *
     * WHAT WE ASSERT
     * The call completes without throwing.  We use `.resolves.toBeUndefined()`
     * because `clearCompleted()` returns `Promise<void>` — a resolved void
     * promise has value `undefined`.
     */

    // The queue is empty (cleared in beforeEach).
    // clearCompleted() should resolve successfully with no error thrown.
    await expect(clearCompleted()).resolves.toBeUndefined();
  });
});
