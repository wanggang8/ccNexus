// API Client for ccNexus
const AUTH_TOKEN_KEY = 'ccnexus_ui_token';
const CURRENT_USER_KEY = 'ccnexus_current_user';

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

            // Return data field if it exists, otherwise return the whole result
            return result.data !== undefined ? result.data : result;
        } catch (error) {
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

    async getEndpointCredentials(name) {
        return this.request('GET', `/endpoints/${encodeURIComponent(name)}/credentials`);
    }

    async importEndpointCredentials(name, data) {
        return this.request('POST', `/endpoints/${encodeURIComponent(name)}/credentials/import`, data);
    }

    async updateEndpointCredential(name, id, data) {
        return this.request('PATCH', `/endpoints/${encodeURIComponent(name)}/credentials/${id}`, data);
    }

    async deleteEndpointCredential(name, id) {
        return this.request('DELETE', `/endpoints/${encodeURIComponent(name)}/credentials/${id}`);
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

    async getUsers() {
        return this.request('GET', '/users');
    }

    async createUser(data) {
        return this.request('POST', '/users', data);
    }

    async resetUserToken(id) {
        return this.request('POST', `/users/${id}/reset-token`);
    }

    async updateUserStatus(id, status) {
        return this.request('PATCH', `/users/${id}/status`, { status });
    }

    // Traffic logs
    async getTrafficLogs(filter = {}) {
        const params = new URLSearchParams();
        if (filter.endpoint) params.append('endpoint', filter.endpoint);
        if (filter.format) params.append('format', filter.format);
        if (filter.status) params.append('status', filter.status);
        if (filter.error !== undefined) params.append('error', filter.error);
        if (filter.limit) params.append('limit', filter.limit);
        
        const query = params.toString();
        return this.request('GET', `/traffic/logs${query ? '?' + query : ''}`);
    }

    async getTrafficLogDetail(id) {
        return this.request('GET', `/traffic/logs?id=${encodeURIComponent(id)}`);
    }

    async setTrafficRecording(enabled) {
        return this.request('POST', '/traffic/recording', { recording: enabled });
    }

    async clearTrafficLogs() {
        return this.request('DELETE', '/traffic/clear');
    }
}

export const api = new APIClient();
export { AUTH_TOKEN_KEY, CURRENT_USER_KEY };
