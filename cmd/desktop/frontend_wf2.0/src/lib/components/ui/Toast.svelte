<script lang="ts">
  import { toast, type Toast } from '$lib/stores/toast';
  import { fly } from 'svelte/transition';

  const iconMap = {
    success: '✅',
    error: '❌',
    warning: '⚠️',
    info: 'ℹ️',
  };

  const colorMap = {
    success: 'bg-green-500',
    error: 'bg-red-500',
    warning: 'bg-yellow-500',
    info: 'bg-blue-500',
  };
</script>

<div class="fixed top-4 right-4 z-50 flex flex-col gap-2 pointer-events-none">
  {#each $toast as t (t.id)}
    <div
      class="pointer-events-auto flex items-center gap-3 px-4 py-3 rounded-lg shadow-lg bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 max-w-sm"
      transition:fly={{ x: 100, duration: 300 }}
    >
      <span class="text-lg">{iconMap[t.type]}</span>
      <p class="text-sm text-gray-700 dark:text-gray-300 flex-1">{t.message}</p>
      <button
        class="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
        on:click={() => toast.remove(t.id)}
      >
        ✕
      </button>
    </div>
  {/each}
</div>
