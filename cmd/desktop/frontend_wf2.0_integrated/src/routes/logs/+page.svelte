<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { t } from '$lib/i18n';
	import { isWailsEnv, logApi } from '$lib/api/wails';
	import { toast } from '$lib/stores/toast';

	let autoScroll = true;
	let logs: string[] = [];
	let logLevel = 0;
	let loading = false;
	let refreshInterval: ReturnType<typeof setInterval> | null = null;

	const logLevels = [
		{ value: 0, label: '全部' },
		{ value: 1, label: '调试' },
		{ value: 2, label: '信息' },
		{ value: 3, label: '警告' },
		{ value: 4, label: '错误' },
	];

	onMount(async () => {
		await loadLogs();
		// 每 2 秒刷新日志
		refreshInterval = setInterval(loadLogs, 2000);
	});

	onDestroy(() => {
		if (refreshInterval) {
			clearInterval(refreshInterval);
		}
	});

	async function loadLogs() {
		if (!isWailsEnv()) return;
		try {
			logs = logLevel === 0 
				? await logApi.getLogs()
				: await logApi.getLogsByLevel(logLevel);
		} catch (e) {
			console.error('Failed to load logs:', e);
		}
	}

	async function clearLogs() {
		if (isWailsEnv()) {
			const success = await logApi.clearLogs();
			if (success) {
				logs = [];
				toast.success('日志已清空');
			} else {
				toast.error('清空失败');
			}
		} else {
			logs = [];
			toast.success('日志已清空（演示模式）');
		}
	}

	async function handleLevelChange() {
		await loadLogs();
	}
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-bold text-gray-900 dark:text-white">
			{$t('logs.title')}
		</h1>
		<div class="flex items-center gap-4">
			<select
				bind:value={logLevel}
				on:change={handleLevelChange}
				class="px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white text-sm"
			>
				{#each logLevels as level}
					<option value={level.value}>{level.label}</option>
				{/each}
			</select>
			<label class="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-400">
				<input type="checkbox" bind:checked={autoScroll} class="rounded" />
				自动滚动
			</label>
			<button
				class="px-4 py-2 bg-red-500 text-white rounded-lg hover:bg-red-600 transition-colors"
				on:click={clearLogs}
			>
				🗑️ 清空
			</button>
		</div>
	</div>

	<div class="bg-gray-900 rounded-xl shadow-sm p-4 h-[calc(100vh-250px)] overflow-auto font-mono text-sm">
		{#if logs.length === 0}
			<p class="text-gray-500">暂无日志...</p>
		{:else}
			{#each logs as log}
				<p class="text-green-400">{log}</p>
			{/each}
		{/if}
	</div>
</div>
