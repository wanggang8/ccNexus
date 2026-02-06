import { writable, derived } from 'svelte/store';
import { statsApi, type Stats } from '$lib/api/wails';

interface StatsState {
  daily: Stats | null;
  weekly: Stats | null;
  monthly: Stats | null;
  loading: boolean;
  error: string | null;
}

function createStatsStore() {
  const { subscribe, set, update } = writable<StatsState>({
    daily: null,
    weekly: null,
    monthly: null,
    loading: false,
    error: null,
  });

  return {
    subscribe,

    async loadDaily() {
      update((state) => ({ ...state, loading: true }));
      try {
        const daily = await statsApi.getDailyStats();
        update((state) => ({ ...state, daily, loading: false }));
      } catch (e) {
        update((state) => ({ ...state, loading: false, error: String(e) }));
      }
    },

    async loadWeekly() {
      update((state) => ({ ...state, loading: true }));
      try {
        const weekly = await statsApi.getWeeklyStats();
        update((state) => ({ ...state, weekly, loading: false }));
      } catch (e) {
        update((state) => ({ ...state, loading: false, error: String(e) }));
      }
    },

    async loadMonthly() {
      update((state) => ({ ...state, loading: true }));
      try {
        const monthly = await statsApi.getMonthlyStats();
        update((state) => ({ ...state, monthly, loading: false }));
      } catch (e) {
        update((state) => ({ ...state, loading: false, error: String(e) }));
      }
    },

    async loadAll() {
      update((state) => ({ ...state, loading: true }));
      try {
        const [daily, weekly, monthly] = await Promise.all([
          statsApi.getDailyStats(),
          statsApi.getWeeklyStats(),
          statsApi.getMonthlyStats(),
        ]);
        set({ daily, weekly, monthly, loading: false, error: null });
      } catch (e) {
        update((state) => ({ ...state, loading: false, error: String(e) }));
      }
    },
  };
}

export const statsStore = createStatsStore();

export const dailyStats = derived(statsStore, ($store) => $store.daily);
export const weeklyStats = derived(statsStore, ($store) => $store.weekly);
export const monthlyStats = derived(statsStore, ($store) => $store.monthly);
export const statsLoading = derived(statsStore, ($store) => $store.loading);
