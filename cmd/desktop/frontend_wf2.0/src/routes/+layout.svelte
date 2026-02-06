<script lang="ts">
	import { onMount } from 'svelte';
	import '../app.css';
	import Header from '$lib/components/layout/Header.svelte';
	import Sidebar from '$lib/components/layout/Sidebar.svelte';
	import Toast from '$lib/components/ui/Toast.svelte';
	import { settingsStore } from '$lib/stores/settings';
	import { isWailsEnv } from '$lib/api/wails';

	let { children } = $props();
	let port = $state(3000);

	onMount(async () => {
		if (isWailsEnv()) {
			await settingsStore.load();
		}
		settingsStore.subscribe((state) => {
			port = state.port;
			// Apply theme
			document.documentElement.classList.remove('dark');
			if (state.theme === 'dark') {
				document.documentElement.classList.add('dark');
			}
		});
	});
</script>

<svelte:head>
	<title>ccNexus 2.0</title>
</svelte:head>

<div class="h-screen flex flex-col">
	<Header {port} />
	<div class="flex flex-1 overflow-hidden">
		<Sidebar />
		<main class="flex-1 overflow-auto p-6 bg-gray-50 dark:bg-gray-900">
			{@render children()}
		</main>
	</div>
</div>

<Toast />
