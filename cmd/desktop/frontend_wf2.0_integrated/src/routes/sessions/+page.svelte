<script lang="ts">
	import { onMount } from 'svelte';
	import { t } from '$lib/i18n';
	import { isWailsEnv, sessionApi } from '$lib/api/wails';
	import { toast } from '$lib/stores/toast';
	import Modal from '$lib/components/ui/Modal.svelte';

	interface Session {
		id: string;
		name: string;
		projectDir: string;
		createdAt: string;
		lastUsed: string;
	}

	let sessions: Session[] = [];
	let loading = false;
	let selectedSession: Session | null = null;
	let showDetailModal = false;
	let sessionData: any = null;

	onMount(async () => {
		await loadSessions();
	});

	async function loadSessions() {
		if (!isWailsEnv()) return;
		loading = true;
		try {
			// 这里需要获取项目目录列表，暂时使用空字符串
			sessions = await sessionApi.getSessions('');
		} finally {
			loading = false;
		}
	}

	async function handleViewSession(session: Session) {
		selectedSession = session;
		sessionData = await sessionApi.getSessionData(session.projectDir, session.id);
		showDetailModal = true;
	}

	async function handleDeleteSession(session: Session) {
		if (confirm('确定要删除这个会话吗？')) {
			const success = await sessionApi.deleteSession(session.projectDir, session.id);
			if (success) {
				toast.success('会话已删除');
				await loadSessions();
			} else {
				toast.error('删除失败');
			}
		}
	}

	async function handleLaunchTerminal(session: Session) {
		const success = await sessionApi.launchSessionTerminal(session.projectDir, session.id);
		if (!success) {
			toast.error('无法打开终端');
		}
	}
</script>

<div class="space-y-6">
	<h1 class="text-2xl font-bold text-gray-900 dark:text-white">
		{$t('sessions.title')}
	</h1>

	{#if sessions.length === 0}
		<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-12 text-center">
			<div class="text-6xl mb-4">💬</div>
			<h3 class="text-lg font-medium text-gray-900 dark:text-white mb-2">
				暂无会话
			</h3>
			<p class="text-gray-500 dark:text-gray-400">
				会话将在 API 请求时自动创建
			</p>
		</div>
	{:else}
		<div class="grid gap-4">
			{#each sessions as session}
				<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm p-4 hover:shadow-md transition-shadow">
					<div class="flex items-center justify-between">
						<div>
							<h3 class="font-medium text-gray-900 dark:text-white">{session.name || session.id}</h3>
							<p class="text-sm text-gray-500 dark:text-gray-400">
								{session.projectDir} • {session.lastUsed || session.createdAt}
							</p>
						</div>
						<div class="flex gap-2">
							<button
								class="px-3 py-1 text-sm bg-indigo-500 text-white rounded-lg hover:bg-indigo-600"
								on:click={() => handleLaunchTerminal(session)}
							>
								💻 打开
							</button>
							<button
								class="px-3 py-1 text-sm bg-gray-100 dark:bg-gray-700 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600"
								on:click={() => handleViewSession(session)}
							>
								查看
							</button>
							<button
								class="px-3 py-1 text-sm text-red-500 hover:bg-red-50 dark:hover:bg-red-900/20 rounded-lg"
								on:click={() => handleDeleteSession(session)}
							>
								删除
							</button>
						</div>
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>

<Modal open={showDetailModal} title="会话详情" size="lg" on:close={() => (showDetailModal = false)}>
	{#if selectedSession && sessionData}
		<div class="space-y-4">
			<div>
				<p class="text-sm text-gray-500">会话 ID</p>
				<p class="font-mono text-sm">{selectedSession.id}</p>
			</div>
			<div>
				<p class="text-sm text-gray-500">项目目录</p>
				<p class="font-mono text-sm">{selectedSession.projectDir}</p>
			</div>
			<div>
				<p class="text-sm text-gray-500">消息数量</p>
				<p>{sessionData?.messages?.length ?? 0}</p>
			</div>
		</div>
	{:else}
		<p class="text-gray-500">加载中...</p>
	{/if}
</Modal>
