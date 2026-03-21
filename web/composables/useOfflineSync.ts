import { initSyncEngine, scheduleSyncSoon } from '~/lib/sync-engine';
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
    });
    window.addEventListener('offline', () => {
      isOnline.value = false;
    });

    initSyncEngine();
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
