import { db } from './db';
import * as queue from './sync-queue';
import { api } from './api';

let syncing = false;
let syncTimer: ReturnType<typeof setTimeout> | null = null;

const BASE_DELAY = 1000;
const MAX_DELAY = 30000;

/**
 * Initialize the sync engine.
 * Listens for online/offline events and triggers sync.
 */
export function initSyncEngine(): void {
  window.addEventListener('online', () => {
    scheduleSyncSoon();
  });
  window.addEventListener('offline', () => {
    cancelScheduledSync();
  });

  // Try sync on init if online
  if (navigator.onLine) {
    scheduleSyncSoon();
  }
}

/**
 * Schedule a sync attempt soon (debounced).
 */
export function scheduleSyncSoon(): void {
  cancelScheduledSync();
  syncTimer = setTimeout(() => {
    void flushQueue();
  }, 500);
}

function cancelScheduledSync(): void {
  if (syncTimer !== null) {
    clearTimeout(syncTimer);
    syncTimer = null;
  }
}

/**
 * Flush the sync queue: send all pending mutations to the server.
 */
async function flushQueue(): Promise<void> {
  if (syncing || !navigator.onLine) return;
  syncing = true;

  try {
    const pending = await queue.getPending();
    if (pending.length === 0) {
      syncing = false;
      return;
    }

    // Build batch payload
    const mutations = pending
      .filter((m) => !queue.isExhausted(m))
      .map((m) => ({
        type: m.entityType,
        action: m.action,
        client_id: m.clientId,
        client_timestamp: m.clientTimestamp,
        data: m.data,
      }));

    if (mutations.length === 0) {
      syncing = false;
      return;
    }

    // Mark all as syncing
    for (const m of pending) {
      if (m.id !== undefined) await queue.markSyncing(m.id);
    }

    // Send batch
    const response = await api<SyncPushResponse>('/sync/push', {
      method: 'POST',
      body: {
        device_id: await getDeviceId(),
        last_sync_at: await getLastSyncAt(),
        mutations,
      },
    });

    // Process results
    for (const result of response.results) {
      const mutation = pending.find((m) => m.clientId === result.client_id);
      if (mutation?.id === undefined) continue;

      if (result.status === 'synced' || result.status === 'conflict') {
        await queue.markSynced(mutation.id);

        // Update local cache with server ID
        if (
          result.server_id !== undefined &&
          result.server_id !== '' &&
          mutation.entityType === 'grade'
        ) {
          await db.grades.where('id').equals(mutation.clientId).modify({
            id: result.server_id,
            serverId: result.server_id,
          });
        }
      } else {
        await queue.markFailed(mutation.id, result.error ?? 'Unknown error');
      }
    }

    // Update last sync timestamp
    if (response.server_timestamp !== '') {
      await db.syncMeta.put({ key: 'lastSyncAt', value: response.server_timestamp });
    }
  } catch (error) {
    // Network error — mark all back as pending with exponential backoff
    const pending = await queue.getPending();
    for (const m of pending) {
      if (m.id !== undefined) {
        await queue.markFailed(m.id, String(error));
      }
    }

    // Schedule retry with backoff
    const firstAttempts = pending[0]?.attempts ?? 0;
    const delay = Math.min(BASE_DELAY * Math.pow(2, firstAttempts), MAX_DELAY);
    syncTimer = setTimeout(() => {
      void flushQueue();
    }, delay);
  } finally {
    syncing = false;
  }
}

// ── Helpers ──

interface SyncPushResponse {
  results: Array<{
    client_id: string;
    status: 'synced' | 'conflict' | 'error';
    server_id?: string;
    error?: string;
  }>;
  server_timestamp: string;
}

async function getDeviceId(): Promise<string> {
  const existing = await db.syncMeta.get('deviceId');
  if (existing !== undefined) return existing.value;

  const deviceId = crypto.randomUUID();
  await db.syncMeta.put({ key: 'deviceId', value: deviceId });
  return deviceId;
}

async function getLastSyncAt(): Promise<string> {
  const existing = await db.syncMeta.get('lastSyncAt');
  return existing?.value ?? new Date(0).toISOString();
}
