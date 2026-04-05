// API Client for ccNexus
class APIClient {
    constructor(baseURL = '/api') {
        this.baseURL = baseURL;
    }

    async request(method, path, data = null) {
        const options = {
            method,
            headers: {
                'Content-Type': 'application/json'
            }
        };

        if (data) {
            options.body = JSON.stringify(data);
        }

        try {
            const response = await fetch(`${this.baseURL}${path}`, options);
            const result = await response.json();

            if (!response.ok) {
                throw new Error(result.error || 'Request failed');
            }

            return result.data || result;
        } catch (error) {
            console.error(`API Error [${method} ${path}]:`, error);
            throw error;
        }
    }

    // Endpoint management
    async getEndpoints() {
        return this.request('GET', '/endpoints');
    }

    async createEndpoint(data) {
        return this.request('POST', '/endpoints', data);
    }

    async updateEndpoint(name, data) {
        const q = `name=${encodeURIComponent(name)}`;
        return this.request('PUT', `/endpoints?${q}`, data);
    }

    async deleteEndpoint(name) {
        return this.request('DELETE', `/endpoints?name=${encodeURIComponent(name)}`);
    }

    async toggleEndpoint(name, enabled) {
        const q = `name=${encodeURIComponent(name)}`;
        return this.request('PATCH', `/endpoints/toggle?${q}`, { enabled });
    }

    async testEndpoint(name) {
        return this.request('POST', `/endpoints/test?name=${encodeURIComponent(name)}`);
    }

    async reorderEndpoints(names) {
        return this.request('POST', '/endpoints/reorder', { names });
    }

    async getCurrentEndpoint() {
        return this.request('GET', '/endpoints/current');
    }

    async switchEndpoint(name) {
        return this.request('POST', '/endpoints/switch', { name });
    }

    async fetchModels(apiUrl, apiKey, transformer) {
        return this.request('POST', '/endpoints/fetch-models', { apiUrl, apiKey, transformer });
    }

    async revealEndpointKey(name) {
        return this.request('POST', `/endpoints/reveal-key?name=${encodeURIComponent(name)}`);
    }

    async getEndpointCredentials(name) {
        return this.request('GET', `/endpoints/credentials?name=${encodeURIComponent(name)}`);
    }

    async importEndpointCredentials(name, data) {
        const q = `name=${encodeURIComponent(name)}`;
        return this.request('POST', `/endpoints/credentials/import?${q}`, data);
    }

    async updateEndpointCredential(name, id, data) {
        const q = `name=${encodeURIComponent(name)}`;
        return this.request('PATCH', `/endpoints/credentials/${id}?${q}`, data);
    }

    async deleteEndpointCredential(name, id) {
        const q = `name=${encodeURIComponent(name)}`;
        return this.request('DELETE', `/endpoints/credentials/${id}?${q}`);
    }

    // Statistics
    async getStatsSummary() {
        return this.request('GET', '/stats/summary');
    }

    async getStatsDaily() {
        return this.request('GET', '/stats/daily');
    }

    async getStatsWeekly() {
        return this.request('GET', '/stats/weekly');
    }

    async getStatsMonthly() {
        return this.request('GET', '/stats/monthly');
    }

    async getStatsTrends() {
        return this.request('GET', '/stats/trends');
    }

    // Configuration
    async getConfig() {
        return this.request('GET', '/config');
    }

    async updateConfig(data) {
        return this.request('PUT', '/config', data);
    }

    async getPort() {
        return this.request('GET', '/config/port');
    }

    async updatePort(port) {
        return this.request('PUT', '/config/port', { port });
    }

    async getLogLevel() {
        return this.request('GET', '/config/log-level');
    }

    async updateLogLevel(logLevel) {
        return this.request('PUT', '/config/log-level', { logLevel });
    }

    async getBasicAuthConfig() {
        return this.request('GET', '/config/basic-auth');
    }

    async updateBasicAuthConfig(data) {
        return this.request('PUT', '/config/basic-auth', data);
    }

    async resetBasicAuthPassword() {
        return this.request('POST', '/config/basic-auth/reset-password');
    }

    async revealBasicAuthPassword() {
        return this.request('POST', '/config/basic-auth/reveal-password');
    }

    async getTraffic(params = {}) {
        const query = new URLSearchParams();
        Object.entries(params).forEach(([key, value]) => {
            if (value !== undefined && value !== null && value !== '') {
                query.set(key, String(value));
            }
        });

        const suffix = query.toString() ? `/traffic?${query.toString()}` : '/traffic';
        return this.request('GET', suffix);
    }

    async getTrafficDetail(id) {
        return this.request('GET', `/traffic/${encodeURIComponent(id)}`);
    }

    async getTrafficRecording() {
        return this.request('GET', '/traffic/recording');
    }

    async setTrafficRecording(enabled) {
        return this.request('PUT', '/traffic/recording', { enabled });
    }

    async clearTraffic() {
        return this.request('POST', '/traffic/clear');
    }
}

export const api = new APIClient();
