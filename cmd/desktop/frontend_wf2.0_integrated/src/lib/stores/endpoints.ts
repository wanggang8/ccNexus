import { writable, derived } from 'svelte/store';
import { endpointApi, type Endpoint, type Config } from '$lib/api/wails';

interface EndpointState {
  endpoints: Endpoint[];
  loading: boolean;
  error: string | null;
}

function createEndpointStore() {
  const { subscribe, set, update } = writable<EndpointState>({
    endpoints: [],
    loading: false,
    error: null,
  });

  return {
    subscribe,

    async load() {
      update((state) => ({ ...state, loading: true, error: null }));
      try {
        const config = await endpointApi.getConfig();
        if (config) {
          update((state) => ({
            ...state,
            endpoints: config.endpoints || [],
            loading: false,
          }));
        } else {
          update((state) => ({ ...state, loading: false }));
        }
      } catch (e) {
        update((state) => ({
          ...state,
          loading: false,
          error: String(e),
        }));
      }
    },

    async add(endpoint: Omit<Endpoint, 'enabled'>) {
      const success = await endpointApi.addEndpoint(endpoint);
      if (success) {
        await this.load();
      }
      return success;
    },

    async update(index: number, endpoint: Omit<Endpoint, 'enabled'>) {
      const success = await endpointApi.updateEndpoint(index, endpoint);
      if (success) {
        await this.load();
      }
      return success;
    },

    async remove(index: number) {
      const success = await endpointApi.removeEndpoint(index);
      if (success) {
        await this.load();
      }
      return success;
    },

    async toggle(index: number, enabled: boolean) {
      const success = await endpointApi.toggleEndpoint(index, enabled);
      if (success) {
        await this.load();
      }
      return success;
    },

    async test(index: number) {
      return await endpointApi.testEndpoint(index);
    },

    async reorder(names: string[]) {
      const success = await endpointApi.reorderEndpoints(names);
      if (success) {
        await this.load();
      }
      return success;
    },

    // For development/demo mode without Wails
    setDemo(endpoints: Endpoint[]) {
      set({ endpoints, loading: false, error: null });
    },
  };
}

export const endpointStore = createEndpointStore();

export const endpoints = derived(endpointStore, ($store) => $store.endpoints);
export const endpointsLoading = derived(endpointStore, ($store) => $store.loading);
export const endpointsError = derived(endpointStore, ($store) => $store.error);
export const activeEndpoints = derived(endpointStore, ($store) =>
  $store.endpoints.filter((e) => e.enabled)
);
export const endpointCount = derived(endpointStore, ($store) => ({
  total: $store.endpoints.length,
  active: $store.endpoints.filter((e) => e.enabled).length,
}));
