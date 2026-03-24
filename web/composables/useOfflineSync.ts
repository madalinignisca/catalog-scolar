import { initSyncEngine, scheduleSyncSoon, onSyncFlush } from '~/lib/sync-engine';
import * as queue from '~/lib/sync-queue';

const isOnline = ref(true);
const pendingMutations = ref(0);
const isSyncing = ref(false);

let initialized = false;

export function useOfflineSync() {
  if (import.meta.client && !initialized) {
    initialized = true;

    isOnline.value = navigator.onLine;
    window.addEventListener('online', () => {
      isOnline.value = true;
      // When connectivity is restored, flush any queued mutations.
      // Without this, offline mutations would stay in the queue until the
      // next enqueueMutation() call, which may never happen if the user
      // is just viewing data after reconnecting.
      scheduleSyncSoon();
    });
    window.addEventListener('offline', () => {
      isOnline.value = false;
    });

    initSyncEngine();

    // When the sync engine finishes flushing the queue, refresh the pending
    // count so the SyncStatus UI updates from "Sincronizare (N)" to "Sincronizat".
    onSyncFlush(() => {
      void refreshPendingCount();
    });

    void refreshPendingCount();
  }

  async function refreshPendingCount(): Promise<void> {
    if (!import.meta.client) return;
    pendingMutations.value = await queue.pendingCount();
  }

  async function enqueueMutation(
    entityType: 'grade' | 'absence',
    action: 'create' | 'update' | 'delete',
    data: Record<string, unknown>,
  ): Promise<string> {
    const clientId = await queue.enqueue(entityType, action, data);
    await refreshPendingCount();

    if (navigator.onLine) {
      scheduleSyncSoon();
    }

    return clientId;
  }

  return {
    isOnline: readonly(isOnline),
    pendingMutations: readonly(pendingMutations),
    isSyncing: readonly(isSyncing),
    enqueueMutation,
    refreshPendingCount,
  };
}
