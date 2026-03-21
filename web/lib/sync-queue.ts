import { db, type SyncMutation } from './db';

/**
 * Add a mutation to the offline sync queue.
 * Returns the client ID for tracking.
 */
export async function enqueue(
  entityType: SyncMutation['entityType'],
  action: SyncMutation['action'],
  data: Record<string, unknown>,
): Promise<string> {
  const clientId = crypto.randomUUID();
  const now = new Date().toISOString();

  await db.syncQueue.add({
    clientId,
    entityType,
    action,
    data: { ...data, client_id: clientId },
    clientTimestamp: now,
    attempts: 0,
    status: 'pending',
    createdAt: now,
  });

  return clientId;
}

/**
 * Get all pending mutations, ordered by creation time.
 */
export async function getPending(): Promise<SyncMutation[]> {
  return db.syncQueue.where('status').equals('pending').sortBy('createdAt');
}

/**
 * Mark a mutation as syncing (in-flight).
 */
export async function markSyncing(id: number): Promise<void> {
  await db.syncQueue.update(id, { status: 'syncing' });
}

/**
 * Mark a mutation as successfully synced and remove it.
 */
export async function markSynced(id: number): Promise<void> {
  await db.syncQueue.delete(id);
}

/**
 * Mark a mutation as failed with error, increment attempts.
 */
export async function markFailed(id: number, error: string): Promise<void> {
  const mutation = await db.syncQueue.get(id);
  if (!mutation) return;

  await db.syncQueue.update(id, {
    status: 'pending', // back to pending for retry
    lastError: error,
    attempts: mutation.attempts + 1,
  });
}

/**
 * Get count of pending mutations.
 */
export async function pendingCount(): Promise<number> {
  return db.syncQueue.where('status').anyOf(['pending', 'syncing']).count();
}

/**
 * Clear all synced mutations (cleanup).
 */
export async function clearCompleted(): Promise<void> {
  await db.syncQueue.where('status').equals('synced').delete();
}

const MAX_RETRIES = 5;

/**
 * Check if a mutation has exceeded max retries.
 */
export function isExhausted(mutation: SyncMutation): boolean {
  return mutation.attempts >= MAX_RETRIES;
}
