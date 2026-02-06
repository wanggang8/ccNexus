<script lang="ts">
	import { onMount } from 'svelte';
	import { t } from '$lib/i18n';
	import { endpointStore, endpointCount } from '$lib/stores/endpoints';
	import { statsStore, dailyStats } from '$lib/stores/stats';
	import { isWailsEnv } from '$lib/api/wails';

	onMount(async () => {
		if (isWailsEnv()) {
			await Promise.all([
				endpointStore.load(),
				statsStore.loadDaily(),
			]);
		}
	});

	$: stats = $dailyStats;
	$: epCount = $endpointCount;
</script>

<div class="space-y-6">
	<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-6">
		<h1 class="text-2xl font-bold text-gray-900 dark:text-white mb-2">
			{$t('dashboard.welcome')}
		</h1>
		<p class="text-gray-600 dark:text-gray-400">
			{$t('dashboard.description')}
		</p>
	</div>

	<!-- 统计卡片 -->
	<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
		<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-5">
			<div class="flex items-center justify-between">
				<div>
					<p class="text-sm text-gray-500 dark:text-gray-400">端点数量</p>
					<p class="text-2xl font-bold text-gray-900 dark:text-white mt-1">
						<span class="text-indigo-500">{epCount.active}</span>
						<span class="text-gray-400 text-lg"> / {epCount.total}</span>
					</p>
				</div>
				<div class="w-12 h-12 bg-indigo-100 dark:bg-indigo-900/30 rounded-full flex items-center justify-center">
					<span class="text-2xl">🔗</span>
				</div>
			</div>
		</div>

		<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-5">
			<div class="flex items-center justify-between">
				<div>
					<p class="text-sm text-gray-500 dark:text-gray-400">今日请求</p>
					<p class="text-2xl font-bold text-gray-900 dark:text-white mt-1">
						{stats?.requests?.toLocaleString() ?? 0}
					</p>
				</div>
				<div class="w-12 h-12 bg-green-100 dark:bg-green-900/30 rounded-full flex items-center justify-center">
					<span class="text-2xl">📊</span>
				</div>
			</div>
		</div>

		<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-5">
			<div class="flex items-center justify-between">
				<div>
					<p class="text-sm text-gray-500 dark:text-gray-400">今日 Tokens</p>
					<p class="text-2xl font-bold text-gray-900 dark:text-white mt-1">
						{((stats?.inputTokens ?? 0) + (stats?.outputTokens ?? 0)).toLocaleString()}
					</p>
				</div>
				<div class="w-12 h-12 bg-blue-100 dark:bg-blue-900/30 rounded-full flex items-center justify-center">
					<span class="text-2xl">🔤</span>
				</div>
			</div>
		</div>

		<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-5">
			<div class="flex items-center justify-between">
				<div>
					<p class="text-sm text-gray-500 dark:text-gray-400">输出 Tokens</p>
					<p class="text-2xl font-bold text-gray-900 dark:text-white mt-1">
						{stats?.outputTokens?.toLocaleString() ?? 0}
					</p>
				</div>
				<div class="w-12 h-12 bg-purple-100 dark:bg-purple-900/30 rounded-full flex items-center justify-center">
					<span class="text-2xl">💬</span>
				</div>
			</div>
		</div>
	</div>

	<!-- 快捷操作 -->
	<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-6">
		<h2 class="text-lg font-semibold text-gray-900 dark:text-white mb-4">快捷操作</h2>
		<div class="flex flex-wrap gap-3">
			<a href="/endpoints" class="px-4 py-2 bg-indigo-500 text-white rounded-lg hover:bg-indigo-600 transition-colors">
				➕ 添加端点
			</a>
			<button class="px-4 py-2 bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors">
				🔄 刷新统计
			</button>
			<button class="px-4 py-2 bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors">
				📋 查看日志
			</button>
		</div>
	</div>
</div>
