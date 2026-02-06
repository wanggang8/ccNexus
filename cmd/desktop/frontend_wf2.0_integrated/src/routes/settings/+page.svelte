<script lang="ts">
	import { onMount } from 'svelte';
	import { t, setLocale } from '$lib/i18n';
	import { settingsStore } from '$lib/stores/settings';
	import { isWailsEnv, terminalApi } from '$lib/api/wails';
	import { toast } from '$lib/stores/toast';
	import WebDAVSettings from '$lib/components/settings/WebDAVSettings.svelte';

	let port = 3000;
	let theme = 'light';
	let themeAuto = false;
	let language = 'zh-CN';
	let saving = false;

	const themes = [
		{ value: 'light', label: '浅色', icon: '☀️' },
		{ value: 'dark', label: '深色', icon: '🌙' },
		{ value: 'sakura', label: '樱花', icon: '🌸' },
		{ value: 'ocean', label: '海洋', icon: '🌊' },
		{ value: 'cyberpunk', label: '赛博朋克', icon: '🤖' },
	];

	const languages = [
		{ value: 'zh-CN', label: '简体中文' },
		{ value: 'en', label: 'English' },
	];

	onMount(async () => {
		if (isWailsEnv()) {
			await settingsStore.load();
		}
		settingsStore.subscribe((state) => {
			port = state.port;
			theme = state.theme;
			language = state.language;
		});
	});

	async function handleSavePort() {
		saving = true;
		try {
			if (isWailsEnv()) {
				const success = await settingsStore.setPort(port);
				if (success) toast.success('端口已保存');
				else toast.error('保存失败');
			} else {
				toast.success('端口已保存（演示模式）');
			}
		} finally {
			saving = false;
		}
	}

	async function handleThemeChange(newTheme: string) {
		theme = newTheme;
		if (isWailsEnv()) {
			await settingsStore.setTheme(newTheme);
		}
		// Apply theme immediately
		document.documentElement.classList.remove('dark');
		if (newTheme === 'dark') {
			document.documentElement.classList.add('dark');
		}
	}

	async function handleLanguageChange() {
		if (isWailsEnv()) {
			await settingsStore.setLanguage(language);
		}
		setLocale(language);
	}

	async function handleThemeAutoChange() {
		if (isWailsEnv()) {
			await settingsStore.setThemeAuto(themeAuto);
		}
		if (themeAuto) {
			// 检测系统主题
			const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
			document.documentElement.classList.toggle('dark', prefersDark);
		}
	}

	async function handleOpenTerminal() {
		if (isWailsEnv()) {
			const success = await terminalApi.openTerminal();
			if (!success) toast.error('无法打开终端');
		} else {
			toast.info('终端功能仅在桌面应用中可用');
		}
	}
</script>

<div class="space-y-6 max-w-2xl">
	<h1 class="text-2xl font-bold text-gray-900 dark:text-white">
		{$t('settings.title')}
	</h1>

	<div class="bg-white dark:bg-gray-800 rounded-xl shadow-sm divide-y divide-gray-200 dark:divide-gray-700">
		<!-- 端口设置 -->
		<div class="p-6">
			<h3 class="text-lg font-medium text-gray-900 dark:text-white mb-4">代理端口</h3>
			<div class="flex items-center gap-4">
				<input
					type="number"
					bind:value={port}
					class="w-32 px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-primary-500 focus:border-transparent"
				/>
				<button
					class="px-4 py-2 bg-indigo-500 text-white rounded-lg hover:bg-indigo-600 transition-colors disabled:opacity-50"
					on:click={handleSavePort}
					disabled={saving}
				>
					{saving ? '保存中...' : '保存'}
				</button>
			</div>
		</div>

		<!-- 主题设置 -->
		<div class="p-6">
			<div class="flex items-center justify-between mb-4">
				<h3 class="text-lg font-medium text-gray-900 dark:text-white">主题</h3>
				<label class="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-400">
					<input
						type="checkbox"
						bind:checked={themeAuto}
						on:change={handleThemeAutoChange}
						class="rounded"
					/>
					跟随系统
				</label>
			</div>
			<div class="grid grid-cols-2 md:grid-cols-5 gap-3" class:opacity-50={themeAuto}>
				{#each themes as t}
					<button
						class="p-3 rounded-lg border-2 transition-colors
							{theme === t.value 
								? 'border-indigo-500 bg-indigo-50 dark:bg-indigo-900/30' 
								: 'border-gray-200 dark:border-gray-700 hover:border-gray-300 dark:hover:border-gray-600'}"
						on:click={() => handleThemeChange(t.value)}
						disabled={themeAuto}
					>
						<span class="text-2xl">{t.icon}</span>
						<p class="text-sm mt-1 text-gray-700 dark:text-gray-300">{t.label}</p>
					</button>
				{/each}
			</div>
		</div>

		<!-- 语言设置 -->
		<div class="p-6">
			<h3 class="text-lg font-medium text-gray-900 dark:text-white mb-4">语言</h3>
			<select
				bind:value={language}
				on:change={handleLanguageChange}
				class="px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-primary-500 focus:border-transparent"
			>
				{#each languages as lang}
					<option value={lang.value}>{lang.label}</option>
				{/each}
			</select>
		</div>

		<!-- 终端 -->
		<div class="p-6">
			<h3 class="text-lg font-medium text-gray-900 dark:text-white mb-4">终端</h3>
			<p class="text-gray-500 dark:text-gray-400 mb-4">打开 Claude Code 终端</p>
			<button
				class="px-4 py-2 bg-gray-900 text-white rounded-lg hover:bg-gray-800 transition-colors"
				on:click={handleOpenTerminal}
			>
				💻 打开终端
			</button>
		</div>

		<!-- WebDAV 设置 -->
		<WebDAVSettings />
	</div>
</div>
