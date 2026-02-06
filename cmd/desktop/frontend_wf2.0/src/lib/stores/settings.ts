import { writable } from 'svelte/store';
import { settingsApi, isWailsEnv } from '$lib/api/wails';
import { setLocale } from '$lib/i18n';

interface SettingsState {
  port: number;
  theme: string;
  language: string;
  loading: boolean;
}

function createSettingsStore() {
  const { subscribe, set, update } = writable<SettingsState>({
    port: 3000,
    theme: 'light',
    language: 'zh-CN',
    loading: false,
  });

  return {
    subscribe,

    async load() {
      if (!isWailsEnv()) return;
      
      update((state) => ({ ...state, loading: true }));
      try {
        const [port, theme, language] = await Promise.all([
          settingsApi.getProxyPort(),
          settingsApi.getTheme(),
          settingsApi.getLanguage(),
        ]);
        set({ port, theme, language, loading: false });
        setLocale(language);
      } catch (e) {
        update((state) => ({ ...state, loading: false }));
      }
    },

    async setPort(port: number) {
      const success = await settingsApi.setProxyPort(port);
      if (success) {
        update((state) => ({ ...state, port }));
      }
      return success;
    },

    async setTheme(theme: string) {
      const success = await settingsApi.setTheme(theme);
      if (success) {
        update((state) => ({ ...state, theme }));
        applyTheme(theme);
      }
      return success;
    },

    async setLanguage(language: string) {
      const success = await settingsApi.setLanguage(language);
      if (success) {
        update((state) => ({ ...state, language }));
        setLocale(language);
      }
      return success;
    },

    async setThemeAuto(auto: boolean) {
      const success = await settingsApi.setThemeAuto(auto);
      return success;
    },
  };
}

function applyTheme(theme: string) {
  const root = document.documentElement;
  root.classList.remove('dark', 'light');
  if (theme === 'dark') {
    root.classList.add('dark');
  }
}

export const settingsStore = createSettingsStore();
