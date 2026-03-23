<script setup lang="ts">
const { isOnline, pendingMutations } = useOfflineSync();
</script>

<template>
  <div
    :class="[
      'flex items-center gap-2 rounded-full px-3 py-1 text-xs font-medium transition-colors',
      isOnline
        ? pendingMutations > 0
          ? 'bg-blue-100 text-blue-700'
          : 'bg-green-100 text-green-700'
        : 'bg-yellow-100 text-yellow-700',
    ]"
  >
    <!-- Status dot -->
    <span
      :class="[
        'h-2 w-2 rounded-full',
        isOnline
          ? pendingMutations > 0
            ? 'animate-pulse bg-blue-500'
            : 'bg-green-500'
          : 'bg-yellow-500',
      ]"
    />

    <!-- Label -->
    <span v-if="!isOnline">Offline</span>
    <span v-else-if="pendingMutations > 0">Sincronizare ({{ pendingMutations }})</span>
    <span v-else>Sincronizat</span>
  </div>
</template>
