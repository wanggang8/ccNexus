<script lang="ts">
	import { onMount } from 'svelte';
	import { t } from '$lib/i18n';
	import Modal from '$lib/components/ui/Modal.svelte';
	import { toast } from '$lib/stores/toast';
	import { endpointStore, endpoints as endpointsStore, endpointsLoading } from '$lib/stores/endpoints';
	import { isWailsEnv, endpointApi, type Endpoint } from '$lib/api/wails';

	let showAddModal = false;
	let editingIndex: number = -1;
	let testingIndex: number = -1;
	let testingAll = false;
	let fetchingModels = false;
	let availableModels: string[] = [];

	// 表单数据
	let formData = {
		name: '',
		apiUrl: '',
		transformer: 'claude',
		model: '',
		apiKey: '',
		remark: '',
	};

	const transformers = [
		{ value: 'claude', label: 'Claude API' },
		{ value: 'openai', label: 'OpenAI API' },
		{ value: 'gemini', label: 'Gemini API' },
		{ value: 'cli', label: 'Claude CLI' },
	];

	onMount(async () => {
		if (isWailsEnv()) {
			await endpointStore.load();
		}
	});

	function handleAddEndpoint() {
		editingIndex = -1;
		formData = { name: '', apiUrl: '', transformer: 'claude', model: '', apiKey: '', remark: '' };
		showAddModal = true;
	}

	function handleEditEndpoint(index: number, endpoint: Endpoint) {
		editingIndex = index;
		formData = {
			name: endpoint.name,
			apiUrl: endpoint.apiUrl,
			transformer: endpoint.transformer,
			model: endpoint.model,
			apiKey: endpoint.apiKey,
			remark: endpoint.remark || '',
		};
		showAddModal = true;
	}

	async function handleSaveEndpoint() {
		if (!formData.name || !formData.apiUrl) {
			toast.error('请填写名称和 URL');
			return;
		}

		if (isWailsEnv()) {
			let success: boolean;
			if (editingIndex >= 0) {
				success = await endpointStore.update(editingIndex, formData);
				if (success) toast.success('端点已更新');
			} else {
				success = await endpointStore.add(formData);
				if (success) toast.success('端点已添加');
			}
			if (!success) toast.error('操作失败');
		} else {
			// Demo mode
			toast.success(editingIndex >= 0 ? '端点已更新' : '端点已添加');
		}
		showAddModal = false;
	}

	async function handleDeleteEndpoint(index: number) {
		if (confirm('确定要删除这个端点吗？')) {
			if (isWailsEnv()) {
				const success = await endpointStore.remove(index);
				if (success) toast.success('端点已删除');
				else toast.error('删除失败');
			} else {
				toast.success('端点已删除');
			}
		}
	}

	async function handleToggleEndpoint(index: number, enabled: boolean) {
		if (isWailsEnv()) {
			await endpointStore.toggle(index, !enabled);
		}
	}

	async function handleTestEndpoint(index: number) {
		testingIndex = index;
		try {
			const result = await endpointStore.test(index);
			if (result.success) {
				toast.success('测试成功');
			} else {
				toast.error(result.message || '测试失败');
			}
		} finally {
			testingIndex = -1;
		}
	}

	async function handleTestAllEndpoints() {
		testingAll = true;
		try {
			const result = await endpointApi.testAllEndpoints();
			if (result.success) {
				const passed = result.results.filter((r: any) => r.success).length;
				const total = result.results.length;
				toast.success(`批量测试完成: ${passed}/${total} 通过`);
			} else {
				toast.error('批量测试失败');
			}
		} finally {
			testingAll = false;
		}
	}

	async function handleFetchModels() {
		if (!formData.apiUrl || !formData.apiKey) {
			toast.error('请先填写 URL 和 API Key');
			return;
		}
		fetchingModels = true;
		try {
			const models = await endpointApi.fetchModels(formData.apiUrl, formData.apiKey, formData.transformer);
			if (models.length > 0) {
				availableModels = models;
				toast.success(`获取到 ${models.length} 个模型`);
			} else {
				toast.error('未获取到模型列表');
			}
		} finally {
			fetchingModels = false;
		}
	}

	$: endpoints = $endpointsStore;
	$: loading = $endpointsLoading;
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-bold text-gray-900 dark:text-white">
			{$t('endpoints.title')}
		</h1>
		<div class="flex gap-2">
			{#if endpoints.length > 0}
				<button
					class="px-4 py-2 bg-blue-500 text-white rounded-lg hover:bg-blue-600 transition-colors flex items-center gap-2 disabled:opacity-50"
					on:click={handleTestAllEndpoints}
					disabled={testingAll}
				>
					<span>🧪</span>
					<span>{testingAll ? '测试中...' : '批量测试'}</span>
				</button>
			{/if}
			<button
				class="px-4 py-2 bg-indigo-500 text-white rounded-lg hover:bg-indigo-600 transition-colors flex items-center gap-2"
				on:click={handleAddEndpoint}
			>
				<span>➕</span>
				<span>{$t('endpoints.add')}</span>
			</button>
		</div>
	</div>

	{#if endpoints.length === 0}
		<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-12 text-center">
			<div class="text-6xl mb-4">🔗</div>
			<h3 class="text-lg font-medium text-gray-900 dark:text-white mb-2">
				{$t('endpoints.empty')}
			</h3>
			<p class="text-gray-500 dark:text-gray-400 mb-6">
				添加您的第一个 API 端点开始使用
			</p>
			<button
				class="px-6 py-3 bg-indigo-500 text-white rounded-lg hover:bg-indigo-600 transition-colors"
				on:click={handleAddEndpoint}
			>
				➕ {$t('endpoints.add')}
			</button>
		</div>
	{:else}
		<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm overflow-hidden">
			<table class="w-full">
				<thead class="bg-gray-50 dark:bg-gray-700">
					<tr>
						<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">
							名称
						</th>
						<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">
							URL
						</th>
						<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">
							转换器
						</th>
						<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">
							状态
						</th>
						<th class="px-6 py-3 text-right text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">
							操作
						</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-gray-200 dark:divide-gray-700">
					{#each endpoints as endpoint, index}
						<tr class="hover:bg-gray-50 dark:hover:bg-gray-700/50">
							<td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900 dark:text-white">
								{endpoint.name}
							</td>
							<td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500 dark:text-gray-400 max-w-xs truncate" title={endpoint.apiUrl}>
								{endpoint.apiUrl}
							</td>
							<td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500 dark:text-gray-400">
								{endpoint.transformer}
							</td>
							<td class="px-6 py-4 whitespace-nowrap">
								<button
									class="px-2 py-1 text-xs font-medium rounded-full transition-colors {endpoint.enabled ? 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400 hover:bg-green-200' : 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-400 hover:bg-gray-200'}"
									on:click={() => handleToggleEndpoint(index, endpoint.enabled)}
								>
									{endpoint.enabled ? '启用' : '禁用'}
								</button>
							</td>
							<td class="px-6 py-4 whitespace-nowrap text-right text-sm space-x-2">
								<button
									class="text-blue-500 hover:text-blue-700 disabled:opacity-50"
									on:click={() => handleTestEndpoint(index)}
									disabled={testingIndex === index}
								>
									{testingIndex === index ? '测试中...' : '测试'}
								</button>
								<button class="text-indigo-500 hover:text-indigo-700" on:click={() => handleEditEndpoint(index, endpoint)}>编辑</button>
								<button class="text-red-500 hover:text-red-700" on:click={() => handleDeleteEndpoint(index)}>删除</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>

<Modal open={showAddModal} title={editingIndex >= 0 ? '编辑端点' : '添加端点'} size="lg" on:close={() => (showAddModal = false)}>
	<div class="space-y-4">
		<div>
			<label for="endpoint-name" class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">名称</label>
			<input
				id="endpoint-name"
				type="text"
				bind:value={formData.name}
				placeholder="例如：Claude API"
				class="w-full px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-primary-500 focus:border-transparent"
			/>
		</div>
		<div>
			<label for="endpoint-url" class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">URL</label>
			<input
				id="endpoint-url"
				type="url"
				bind:value={formData.apiUrl}
				placeholder="例如：https://api.anthropic.com"
				class="w-full px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-primary-500 focus:border-transparent"
			/>
		</div>
		<div>
			<label for="endpoint-transformer" class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">转换器</label>
			<select
				id="endpoint-transformer"
				bind:value={formData.transformer}
				class="w-full px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-primary-500 focus:border-transparent"
			>
				{#each transformers as t}
					<option value={t.value}>{t.label}</option>
				{/each}
			</select>
		</div>
		<div>
			<label for="endpoint-model" class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">模型</label>
			<div class="flex gap-2">
				{#if availableModels.length > 0}
					<select
						id="endpoint-model"
						bind:value={formData.model}
						class="flex-1 px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
					>
						<option value="">选择模型...</option>
						{#each availableModels as model}
							<option value={model}>{model}</option>
						{/each}
					</select>
				{:else}
					<input
						id="endpoint-model"
						type="text"
						bind:value={formData.model}
						placeholder="例如：claude-sonnet-4-20250514"
						class="flex-1 px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
					/>
				{/if}
				<button
					type="button"
					class="px-3 py-2 bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors disabled:opacity-50"
					on:click={handleFetchModels}
					disabled={fetchingModels}
				>
					{fetchingModels ? '获取中...' : '📋 获取'}
				</button>
			</div>
		</div>
		<div>
			<label for="endpoint-apikey" class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">API Key</label>
			<input
				id="endpoint-apikey"
				type="password"
				bind:value={formData.apiKey}
				placeholder="sk-..."
				class="w-full px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-primary-500 focus:border-transparent"
			/>
		</div>
		<div>
			<label for="endpoint-remark" class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">备注</label>
			<input
				id="endpoint-remark"
				type="text"
				bind:value={formData.remark}
				placeholder="可选"
				class="w-full px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-primary-500 focus:border-transparent"
			/>
		</div>
	</div>
	<div slot="footer">
		<button
			class="px-4 py-2 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg transition-colors"
			on:click={() => (showAddModal = false)}
		>
			取消
		</button>
		<button
			class="px-4 py-2 bg-indigo-500 text-white rounded-lg hover:bg-indigo-600 transition-colors"
			on:click={handleSaveEndpoint}
		>
			{editingIndex >= 0 ? '保存' : '添加'}
		</button>
	</div>
</Modal>
