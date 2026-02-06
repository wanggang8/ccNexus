<script lang="ts">
	import { onMount } from 'svelte';
	import Modal from '$lib/components/ui/Modal.svelte';
	import { isWailsEnv, settingsApi } from '$lib/api/wails';
	import { locale } from '$lib/i18n';

	export let open = false;

	let changelog = '';
	let loading = false;

	$: if (open) {
		loadChangelog();
	}

	async function loadChangelog() {
		if (!isWailsEnv()) {
			changelog = '# 更新日志\n\n## v2.0.0\n\n- 全新 UI 2.0 设计\n- 性能优化\n- Bug 修复';
			return;
		}
		
		loading = true;
		try {
			const lang = $locale || 'zh-CN';
			changelog = await settingsApi.getChangelog(lang);
		} catch (e) {
			changelog = '无法加载更新日志';
		} finally {
			loading = false;
		}
	}

	function close() {
		open = false;
	}
</script>

<Modal {open} title="更新日志" size="lg" on:close={close}>
	<div class="max-h-96 overflow-auto">
		{#if loading}
			<div class="flex items-center justify-center py-8">
				<span class="animate-spin text-2xl">⏳</span>
				<span class="ml-2 text-gray-500">加载中...</span>
			</div>
		{:else}
			<div class="prose prose-sm dark:prose-invert max-w-none">
				{@html changelog.replace(/\n/g, '<br>').replace(/^# (.+)$/gm, '<h1>$1</h1>').replace(/^## (.+)$/gm, '<h2>$1</h2>').replace(/^- (.+)$/gm, '<li>$1</li>')}
			</div>
		{/if}
	</div>
</Modal>
