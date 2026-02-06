<script lang="ts">
	import { onMount } from 'svelte';
	import { t } from '$lib/i18n';
	import { statsStore, dailyStats, weeklyStats, monthlyStats, statsLoading } from '$lib/stores/stats';
	import { isWailsEnv, statsApi } from '$lib/api/wails';

	let trendData: any[] = [];

	type Period = 'daily' | 'weekly' | 'monthly' | 'history';
	let currentPeriod: Period = 'daily';

	const periods: { key: Period; label: string; icon: string }[] = [
		{ key: 'daily', label: '今日', icon: '📅' },
		{ key: 'weekly', label: '本周', icon: '📊' },
		{ key: 'monthly', label: '本月', icon: '📈' },
		{ key: 'history', label: '历史', icon: '📚' },
	];

	onMount(async () => {
		if (isWailsEnv()) {
			await statsStore.loadAll();
			trendData = await statsApi.getStatsTrend();
		}
	});

	async function loadTrendData() {
		if (isWailsEnv()) {
			trendData = await statsApi.getStatsTrendByPeriod(currentPeriod);
		}
	}

	$: if (currentPeriod) {
		loadTrendData();
	}

	$: currentStats = currentPeriod === 'daily' ? $dailyStats
		: currentPeriod === 'weekly' ? $weeklyStats
		: currentPeriod === 'monthly' ? $monthlyStats
		: null;

	$: loading = $statsLoading;
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-bold text-gray-900 dark:text-white">
			{$t('stats.title')}
		</h1>
		<div class="flex bg-white dark:bg-gray-800 rounded-lg p-1 shadow-sm">
			{#each periods as period}
				<button
					class="px-4 py-2 rounded-md text-sm font-medium transition-colors
						{currentPeriod === period.key 
							? 'bg-indigo-500 text-white' 
							: 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700'}"
					on:click={() => (currentPeriod = period.key)}
				>
					{period.icon} {period.label}
				</button>
			{/each}
		</div>
	</div>

	<div class="grid grid-cols-1 md:grid-cols-3 gap-4">
		<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-6">
			<h3 class="text-sm font-medium text-gray-500 dark:text-gray-400 mb-2">总请求数</h3>
			<p class="text-3xl font-bold text-gray-900 dark:text-white">
				{#if loading}
					<span class="animate-pulse">...</span>
				{:else}
					{currentStats?.requests?.toLocaleString() ?? 0}
				{/if}
			</p>
		</div>
		<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-6">
			<h3 class="text-sm font-medium text-gray-500 dark:text-gray-400 mb-2">输入 Tokens</h3>
			<p class="text-3xl font-bold text-gray-900 dark:text-white">
				{#if loading}
					<span class="animate-pulse">...</span>
				{:else}
					{currentStats?.inputTokens?.toLocaleString() ?? 0}
				{/if}
			</p>
		</div>
		<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-6">
			<h3 class="text-sm font-medium text-gray-500 dark:text-gray-400 mb-2">输出 Tokens</h3>
			<p class="text-3xl font-bold text-gray-900 dark:text-white">
				{#if loading}
					<span class="animate-pulse">...</span>
				{:else}
					{currentStats?.outputTokens?.toLocaleString() ?? 0}
				{/if}
			</p>
		</div>
	</div>

	{#if currentStats?.cacheReadTokens || currentStats?.cacheWriteTokens}
		<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
			<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-6">
				<h3 class="text-sm font-medium text-gray-500 dark:text-gray-400 mb-2">缓存读取 Tokens</h3>
				<p class="text-2xl font-bold text-green-600 dark:text-green-400">
					{currentStats?.cacheReadTokens?.toLocaleString() ?? 0}
				</p>
			</div>
			<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-6">
				<h3 class="text-sm font-medium text-gray-500 dark:text-gray-400 mb-2">缓存写入 Tokens</h3>
				<p class="text-2xl font-bold text-blue-600 dark:text-blue-400">
					{currentStats?.cacheWriteTokens?.toLocaleString() ?? 0}
				</p>
			</div>
		</div>
	{/if}

	<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-6">
		<h2 class="text-lg font-semibold text-gray-900 dark:text-white mb-4">请求趋势</h2>
		<div class="h-64">
			{#if trendData.length === 0}
				<div class="h-full flex items-center justify-center text-gray-400">
					<p class="text-center">
						<span class="text-4xl mb-2 block">📊</span>
						<span>暂无趋势数据</span>
					</p>
				</div>
			{:else}
				<div class="h-full flex items-end gap-1">
					{#each trendData as item, i}
						{@const maxRequests = Math.max(...trendData.map((d: any) => d.requests || 0), 1)}
						{@const height = ((item.requests || 0) / maxRequests) * 100}
						<div class="flex-1 flex flex-col items-center group">
							<div
								class="w-full bg-indigo-500 hover:bg-indigo-600 rounded-t transition-all cursor-pointer"
								style="height: {Math.max(height, 2)}%"
								title="{item.date || item.label}: {item.requests} 请求"
							></div>
							{#if trendData.length <= 12}
								<span class="text-xs text-gray-500 mt-1 truncate w-full text-center">
									{item.label || item.date?.slice(-5) || i + 1}
								</span>
							{/if}
						</div>
					{/each}
				</div>
			{/if}
		</div>
	</div>
</div>
