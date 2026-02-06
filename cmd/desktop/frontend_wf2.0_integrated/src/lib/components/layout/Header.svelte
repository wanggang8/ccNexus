<script lang="ts">
  import { goto } from '$app/navigation';
  import { t } from '$lib/i18n';
  import ChangelogModal from '$lib/components/settings/ChangelogModal.svelte';
  
  export let port: number = 3000;
  
  let showChangelog = false;
  
  function openGitHub() {
    window.open('https://github.com/anthropics/ccNexus', '_blank');
  }
  
  function goToSettings() {
    goto('/settings');
  }
  
  function showAbout() {
    showChangelog = true;
  }
</script>

<header class="bg-white dark:bg-gray-800 shadow-md px-6 py-4">
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-xl font-bold text-indigo-500 flex items-center gap-2">
        <span>🚀</span>
        <span>{$t('app.title')}</span>
      </h1>
      <p class="text-sm text-gray-500 dark:text-gray-400 mt-1">
        {$t('header.title')}
      </p>
    </div>
    
    <div class="flex items-center gap-4">
      <button
        class="px-3 py-1.5 bg-gray-100 dark:bg-gray-700 rounded-lg text-sm font-medium text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
        title={$t('header.port')}
      >
        <span class="text-gray-500">{$t('header.port')}:</span>
        <span class="text-indigo-500 font-bold ml-1">{port}</span>
      </button>
      
      <div class="flex gap-2">
        <button
          class="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
          on:click={openGitHub}
          title={$t('header.githubRepo')}
        >
          <svg class="w-5 h-5 text-gray-600 dark:text-gray-400" viewBox="0 0 16 16" fill="currentColor">
            <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/>
          </svg>
        </button>
        
        <button
          class="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
          on:click={showAbout}
          title={$t('header.about')}
        >
          <span class="text-lg">📖</span>
        </button>
        
        <button
          class="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
          on:click={goToSettings}
          title={$t('settings.title')}
        >
          <span class="text-lg">⚙️</span>
        </button>
      </div>
    </div>
  </div>
</header>

<ChangelogModal bind:open={showChangelog} />
