<script lang="ts">
	import { isWailsEnv, webdavApi } from '$lib/api/wails';
	import { toast } from '$lib/stores/toast';
	import Modal from '$lib/components/ui/Modal.svelte';

	let showModal = false;
	let testing = false;
	let saving = false;
	let backingUp = false;

	let config = {
		url: '',
		username: '',
		password: '',
	};

	let backups: string[] = [];

	async function handleTestConnection() {
		if (!config.url) {
			toast.error('请输入 WebDAV URL');
			return;
		}
		testing = true;
		try {
			const result = await webdavApi.testConnection(config.url, config.username, config.password);
			if (result.success) {
				toast.success('连接成功');
			} else {
				toast.error(result.message || '连接失败');
			}
		} finally {
			testing = false;
		}
	}

	async function handleSaveConfig() {
		if (!config.url) {
			toast.error('请输入 WebDAV URL');
			return;
		}
		saving = true;
		try {
			const success = await webdavApi.updateConfig(config.url, config.username, config.password);
			if (success) {
				toast.success('配置已保存');
				showModal = false;
			} else {
				toast.error('保存失败');
			}
		} finally {
			saving = false;
		}
	}

	async function handleBackup() {
		const filename = `ccnexus-backup-${new Date().toISOString().slice(0, 10)}.json`;
		backingUp = true;
		try {
			const success = await webdavApi.backup(filename);
			if (success) {
				toast.success('备份成功');
				await loadBackups();
			} else {
				toast.error('备份失败');
			}
		} finally {
			backingUp = false;
		}
	}

	async function loadBackups() {
		backups = await webdavApi.listBackups();
	}

	async function handleRestore(filename: string) {
		if (confirm(`确定要从 ${filename} 恢复配置吗？这将覆盖当前配置。`)) {
			const success = await webdavApi.restore(filename, 'merge');
			if (success) {
				toast.success('恢复成功');
			} else {
				toast.error('恢复失败');
			}
		}
	}

	function openModal() {
		showModal = true;
		if (isWailsEnv()) {
			loadBackups();
		}
	}
</script>

<div class="p-6">
	<h3 class="text-lg font-medium text-gray-900 dark:text-white mb-4">WebDAV 同步</h3>
	<p class="text-gray-500 dark:text-gray-400 mb-4">配置 WebDAV 服务器以同步您的设置和端点</p>
	<button
		class="px-4 py-2 bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
		on:click={openModal}
	>
		⚙️ 配置 WebDAV
	</button>
</div>

<Modal open={showModal} title="WebDAV 配置" size="lg" on:close={() => (showModal = false)}>
	<div class="space-y-6">
		<div class="space-y-4">
			<div>
				<label for="webdav-url" class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
					WebDAV URL
				</label>
				<input
					id="webdav-url"
					type="url"
					bind:value={config.url}
					placeholder="https://dav.example.com/path"
					class="w-full px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-primary-500 focus:border-transparent"
				/>
			</div>
			<div>
				<label for="webdav-username" class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
					用户名
				</label>
				<input
					id="webdav-username"
					type="text"
					bind:value={config.username}
					placeholder="username"
					class="w-full px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-primary-500 focus:border-transparent"
				/>
			</div>
			<div>
				<label for="webdav-password" class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
					密码
				</label>
				<input
					id="webdav-password"
					type="password"
					bind:value={config.password}
					placeholder="password"
					class="w-full px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-primary-500 focus:border-transparent"
				/>
			</div>
			<div class="flex gap-2">
				<button
					class="px-4 py-2 bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors disabled:opacity-50"
					on:click={handleTestConnection}
					disabled={testing}
				>
					{testing ? '测试中...' : '🔗 测试连接'}
				</button>
				<button
					class="px-4 py-2 bg-indigo-500 text-white rounded-lg hover:bg-indigo-600 transition-colors disabled:opacity-50"
					on:click={handleSaveConfig}
					disabled={saving}
				>
					{saving ? '保存中...' : '💾 保存配置'}
				</button>
			</div>
		</div>

		<hr class="border-gray-200 dark:border-gray-700" />

		<div>
			<div class="flex items-center justify-between mb-4">
				<h4 class="font-medium text-gray-900 dark:text-white">备份管理</h4>
				<button
					class="px-3 py-1.5 bg-green-500 text-white rounded-lg hover:bg-green-600 transition-colors text-sm disabled:opacity-50"
					on:click={handleBackup}
					disabled={backingUp}
				>
					{backingUp ? '备份中...' : '📤 立即备份'}
				</button>
			</div>
			
			{#if backups.length === 0}
				<p class="text-gray-500 dark:text-gray-400 text-sm">暂无备份文件</p>
			{:else}
				<div class="space-y-2 max-h-48 overflow-auto">
					{#each backups as backup}
						<div class="flex items-center justify-between p-2 bg-gray-50 dark:bg-gray-700/50 rounded-lg">
							<span class="text-sm font-mono">{backup}</span>
							<button
								class="px-2 py-1 text-xs bg-blue-500 text-white rounded hover:bg-blue-600"
								on:click={() => handleRestore(backup)}
							>
								📥 恢复
							</button>
						</div>
					{/each}
				</div>
			{/if}
		</div>
	</div>
</Modal>
