<script lang="ts">
  import { page } from '$app/stores';
  import { t } from '$lib/i18n';
  
  const menuItems = [
    { path: '/', icon: '📊', label: 'nav.dashboard' },
    { path: '/endpoints', icon: '🔗', label: 'nav.endpoints' },
    { path: '/sessions', icon: '💬', label: 'nav.sessions' },
    { path: '/stats', icon: '📈', label: 'nav.stats' },
    { path: '/logs', icon: '📋', label: 'nav.logs' },
    { path: '/settings', icon: '⚙️', label: 'nav.settings' },
  ];
  
  $: currentPath = $page.url.pathname;
</script>

<aside class="w-56 bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700 flex flex-col">
  <nav class="flex-1 p-4">
    <ul class="space-y-1">
      {#each menuItems as item}
        <li>
          <a
            href={item.path}
            class="flex items-center gap-3 px-4 py-2.5 rounded-lg text-sm font-medium transition-colors
              {currentPath === item.path 
                ? 'bg-indigo-500 text-white shadow-md' 
                : 'text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'}"
          >
            <span class="text-lg">{item.icon}</span>
            <span>{$t(item.label)}</span>
          </a>
        </li>
      {/each}
    </ul>
  </nav>
  
  <div class="p-4 border-t border-gray-200 dark:border-gray-700">
    <div class="text-xs text-gray-500 dark:text-gray-400 text-center">
      ccNexus v2.0
    </div>
  </div>
</aside>
