// API Client for ccNexus
const AUTH_TOKEN_KEY = 'ccnexus_ui_token';

class APIClient {
    constructor(baseURL = '/api') {
        this.baseURL = baseURL;
    }

    getAuthHeaders() {
        const headers = { 'Content-Type': 'application/json' };
        const token = sessionStorage.getItem(AUTH_TOKEN_KEY);
        if (token) {
            headers['Authorization'] = `Bearer ${token}`;
            headers['X-API-Token'] = token;
        }
        return headers;
    }

    async request(method, path, data = null) {
        const options = {
            method,
            headers: this.getAuthHeaders()
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
        return this.request('PUT', `/endpoints/${encodeURIComponent(name)}`, data);
    }

    async deleteEndpoint(name) {
        return this.request('DELETE', `/endpoints/${encodeURIComponent(name)}`);
    }

    async toggleEndpoint(name, enabled) {
        return this.request('PATCH', `/endpoints/${encodeURIComponent(name)}/toggle`, { enabled });
    }

    async testEndpoint(name) {
        return this.request('POST', `/endpoints/${encodeURIComponent(name)}/test`);
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

    async fetchModels(apiUrl, apiKey, transformer, endpointName = null) {
        const body = { apiUrl, apiKey, transformer };
        if (endpointName) body.endpointName = endpointName;
        return this.request('POST', '/endpoints/fetch-models', body);
    }

    async exportEndpoints() {
        return this.request('GET', '/endpoints/export');
    }

    async importEndpoints(endpoints, mode = 'merge') {
        return this.request('POST', '/endpoints/import', { endpoints, mode });
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

    // Auth (no token required)
    async getAuthStatus() {
        const res = await fetch(`${this.baseURL}/auth/status`);
        const data = await res.json();
        return data.authRequired !== undefined ? data : { authRequired: false };
    }

    async verifyToken(token) {
        const res = await fetch(`${this.baseURL}/auth/verify`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ token })
        });
        const data = await res.json();
        return { ok: res.ok, ...data };
    }
}

export const api = new APIClient();
export { AUTH_TOKEN_KEY };
