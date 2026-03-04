import { api } from '../api.js';
import { notifications } from '../utils/notifications.js';
import { getIcon } from '../icons.js';

class Traffic {
    constructor() {
        this.container = document.getElementById('view-container');
        this.logs = [];
        this.recording = false;
        this.filter = {};
        this.autoRefresh = false;
        this.refreshInterval = null;
    }

    async render() {
        this.container.innerHTML = `
            <div class="traffic">
                <div class="flex justify-between align-center mb-3">
                    <h1>Traffic Logs</h1>
                    <div class="flex gap-2">
                        <button id="auto-refresh-btn" class="btn btn-sm btn-secondary" title="Auto refresh">
                            <span class="icon">${getIcon('refresh')}</span>
                            <span>Auto</span>
                        </button>
                        <button id="refresh-btn" class="btn btn-sm btn-secondary" title="Refresh">
                            <span class="icon">${getIcon('refresh')}</span>
                        </button>
                        <button id="clear-btn" class="btn btn-sm btn-danger" title="Clear logs">
                            <span class="icon">${getIcon('trash')}</span>
                            <span>Clear</span>
                        </button>
                        <button id="recording-toggle" class="btn btn-sm btn-secondary">
                            <span class="icon">${getIcon('activity')}</span>
                            <span id="recording-text">Start Recording</span>
                        </button>
                    </div>
                </div>

                <div class="card mb-3">
                    <div class="card-body">
                        <div class="flex gap-2 align-center">
                            <span class="text-sm text-muted">Filter:</span>
                            <select id="filter-endpoint" class="form-select form-select-sm">
                                <option value="">All Endpoints</option>
                            </select>
                            <select id="filter-format" class="form-select form-select-sm">
                                <option value="">All Formats</option>
                                <option value="openai">OpenAI</option>
                                <option value="claude">Claude</option>
                                <option value="gemini">Gemini</option>
                            </select>
                            <select id="filter-status" class="form-select form-select-sm">
                                <option value="">All Status</option>
                                <option value="200">200 OK</option>
                                <option value="400">400 Bad Request</option>
                                <option value="401">401 Unauthorized</option>
                                <option value="500">500 Server Error</option>
                            </select>
                            <select id="filter-error" class="form-select form-select-sm">
                                <option value="">All</option>
                                <option value="true">Errors Only</option>
                                <option value="false">Success Only</option>
                            </select>
                            <button id="reset-filter-btn" class="btn btn-sm btn-secondary">Reset</button>
                        </div>
                    </div>
                </div>

                <div class="card">
                    <div class="card-body">
                        <div id="traffic-stats" class="mb-3"></div>
                        <div id="traffic-list"></div>
                    </div>
                </div>
            </div>
        `;

        this.setupEventListeners();
        await this.loadLogs();
        await this.loadEndpoints();
    }

    setupEventListeners() {
        // Recording toggle
        document.getElementById('recording-toggle').addEventListener('click', () => {
            this.toggleRecording();
        });

        // Refresh
        document.getElementById('refresh-btn').addEventListener('click', () => {
            this.loadLogs();
        });

        // Auto refresh
        document.getElementById('auto-refresh-btn').addEventListener('click', () => {
            this.toggleAutoRefresh();
        });

        // Clear logs
        document.getElementById('clear-btn').addEventListener('click', () => {
            this.clearLogs();
        });

        // Filters
        ['filter-endpoint', 'filter-format', 'filter-status', 'filter-error'].forEach(id => {
            document.getElementById(id).addEventListener('change', (e) => {
                const key = id.replace('filter-', '');
                this.filter[key] = e.target.value;
                this.loadLogs();
            });
        });

        // Reset filter
        document.getElementById('reset-filter-btn').addEventListener('click', () => {
            this.filter = {};
            document.getElementById('filter-endpoint').value = '';
            document.getElementById('filter-format').value = '';
            document.getElementById('filter-status').value = '';
            document.getElementById('filter-error').value = '';
            this.loadLogs();
        });
    }

    async loadEndpoints() {
        try {
            const data = await api.getEndpoints();
            const select = document.getElementById('filter-endpoint');
            data.endpoints.forEach(ep => {
                const option = document.createElement('option');
                option.value = ep.name;
                option.textContent = ep.name;
                select.appendChild(option);
            });
        } catch (error) {
            console.error('Failed to load endpoints:', error);
        }
    }

    async loadLogs() {
        try {
            const data = await api.getTrafficLogs(this.filter);
            this.logs = data.logs || [];
            this.recording = data.recording || false;
            this.updateRecordingButton();
            this.updateStats(data);
            this.renderLogs();
        } catch (error) {
            notifications.error('Failed to load traffic logs: ' + error.message);
        }
    }

    updateRecordingButton() {
        const btn = document.getElementById('recording-toggle');
        const text = document.getElementById('recording-text');
        if (!btn || !text) return; // 添加空指针检查
        
        if (this.recording) {
            btn.classList.remove('btn-secondary');
            btn.classList.add('btn-danger');
            text.textContent = 'Stop Recording';
        } else {
            btn.classList.remove('btn-danger');
            btn.classList.add('btn-secondary');
            text.textContent = 'Start Recording';
        }
    }

    updateStats(data) {
        const statsEl = document.getElementById('traffic-stats');
        statsEl.innerHTML = `
            <div class="flex gap-3 text-sm">
                <span><strong>Total:</strong> ${data.total || 0}</span>
                <span><strong>Filtered:</strong> ${data.count || 0}</span>
                <span class="recording-status ${this.recording ? 'recording' : ''}">
                    <span class="recording-dot"></span>
                    ${this.recording ? 'Recording' : 'Not Recording'}
                </span>
            </div>
        `;
    }

    renderLogs() {
        const listEl = document.getElementById('traffic-list');
        
        if (this.logs.length === 0) {
            listEl.innerHTML = '<div class="empty-state"><p>No traffic logs</p></div>';
            return;
        }

        const logsHTML = this.logs.map(log => this.renderLogItem(log)).join('');
        listEl.innerHTML = `<div class="traffic-logs-list">${logsHTML}</div>`;

        // Add click handlers
        document.querySelectorAll('.traffic-log-item').forEach(item => {
            item.addEventListener('click', () => {
                this.showLogDetail(item.dataset.id);
            });
        });
    }

    renderLogItem(log) {
        const timestamp = new Date(log.timestamp).toLocaleString();
        const duration = log.duration ? `${log.duration}ms` : '-';
        const statusClass = log.statusCode >= 400 ? 'status-error' : 'status-success';
        const streamBadge = log.isStreaming ? '<span class="badge badge-info">Stream</span>' : '';
        const truncatedBadge = log.truncated ? '<span class="badge badge-warning">Truncated</span>' : '';
        const errorBadge = log.error ? '<span class="badge badge-danger">Error</span>' : '';

        return `
            <div class="traffic-log-item" data-id="${log.id}">
                <div class="traffic-log-header">
                    <span class="traffic-log-time">${timestamp}</span>
                    <span class="traffic-log-endpoint">${log.endpointName}</span>
                    <span class="traffic-log-format">${log.clientFormat}</span>
                    <span class="traffic-log-status ${statusClass}">${log.statusCode}</span>
                    <span class="traffic-log-duration">${duration}</span>
                    ${streamBadge}
                    ${truncatedBadge}
                    ${errorBadge}
                </div>
                <div class="traffic-log-details">
                    <span>${log.method} ${log.path}</span>
                    ${log.transformerName ? `<span class="text-muted">→ ${log.transformerName}</span>` : ''}
                    ${log.inputTokens || log.outputTokens ? `<span class="text-muted">Tokens: ${log.inputTokens}/${log.outputTokens}</span>` : ''}
                </div>
                ${log.error ? `<div class="traffic-log-error">${log.error}</div>` : ''}
            </div>
        `;
    }

    async showLogDetail(id) {
        try {
            const detail = await api.getTrafficLogDetail(id);
            this.renderDetailModal(detail);
        } catch (error) {
            notifications.error('Failed to load log detail: ' + error.message);
        }
    }

    renderDetailModal(detail) {
        const modal = document.getElementById('modal-container');
        const timestamp = new Date(detail.timestamp).toLocaleString();

        modal.innerHTML = `
            <div class="modal-overlay" id="detail-modal-overlay">
                <div class="modal modal-lg">
                    <div class="modal-header">
                        <h2>Traffic Log Detail</h2>
                        <button class="btn-icon" id="close-detail-modal">
                            <span class="icon">${getIcon('x')}</span>
                        </button>
                    </div>
                    <div class="modal-body">
                        <div class="traffic-detail">
                            <div class="traffic-detail-section">
                                <h3>Overview</h3>
                                <table class="detail-table">
                                    <tr><td>Time:</td><td>${timestamp}</td></tr>
                                    <tr><td>Endpoint:</td><td>${detail.endpointName}</td></tr>
                                    <tr><td>Format:</td><td>${detail.clientFormat}</td></tr>
                                    <tr><td>Transformer:</td><td>${detail.transformerName || '-'}</td></tr>
                                    <tr><td>Method:</td><td>${detail.method}</td></tr>
                                    <tr><td>Path:</td><td>${detail.path}</td></tr>
                                    <tr><td>Status:</td><td>${detail.statusCode}</td></tr>
                                    <tr><td>Duration:</td><td>${detail.duration}ms</td></tr>
                                    <tr><td>Input Tokens:</td><td>${detail.inputTokens || 0}</td></tr>
                                    <tr><td>Output Tokens:</td><td>${detail.outputTokens || 0}</td></tr>
                                    <tr><td>Streaming:</td><td>${detail.isStreaming ? 'Yes' : 'No'}</td></tr>
                                    ${detail.truncated ? '<tr><td colspan="2" class="text-warning">⚠️ Body truncated (exceeded 512KB)</td></tr>' : ''}
                                </table>
                            </div>

                            ${detail.error ? `
                                <div class="traffic-detail-section">
                                    <h3>Error</h3>
                                    <pre class="code-block error-block">${this.escapeHtml(detail.error)}</pre>
                                </div>
                            ` : ''}

                            <div class="traffic-detail-section">
                                <h3>Original Request</h3>
                                <pre class="code-block">${this.formatJSON(detail.originalRequest)}</pre>
                            </div>

                            ${detail.transformedRequest ? `
                                <div class="traffic-detail-section">
                                    <h3>Transformed Request</h3>
                                    <pre class="code-block">${this.formatJSON(detail.transformedRequest)}</pre>
                                </div>
                            ` : ''}

                            <div class="traffic-detail-section">
                                <h3>Original Response</h3>
                                <pre class="code-block">${this.formatJSON(detail.originalResponse)}</pre>
                            </div>

                            ${detail.transformedResponse ? `
                                <div class="traffic-detail-section">
                                    <h3>Transformed Response</h3>
                                    <pre class="code-block">${this.formatJSON(detail.transformedResponse)}</pre>
                                </div>
                            ` : ''}
                        </div>
                    </div>
                </div>
            </div>
        `;

        document.getElementById('close-detail-modal').addEventListener('click', () => {
            modal.innerHTML = '';
        });

        document.getElementById('detail-modal-overlay').addEventListener('click', (e) => {
            if (e.target.id === 'detail-modal-overlay') {
                modal.innerHTML = '';
            }
        });
    }

    formatJSON(str) {
        if (!str) return '<span class="text-muted">Empty</span>';
        try {
            const obj = JSON.parse(str);
            return this.escapeHtml(JSON.stringify(obj, null, 2));
        } catch {
            return this.escapeHtml(str);
        }
    }

    escapeHtml(str) {
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }

    async toggleRecording() {
        try {
            const newState = !this.recording;
            await api.setTrafficRecording(newState);
            this.recording = newState;
            this.updateRecordingButton();
            notifications.success(`Recording ${newState ? 'enabled' : 'disabled'}`);
        } catch (error) {
            notifications.error('Failed to toggle recording: ' + error.message);
        }
    }

    toggleAutoRefresh() {
        this.autoRefresh = !this.autoRefresh;
        const btn = document.getElementById('auto-refresh-btn');
        
        if (this.autoRefresh) {
            btn.classList.remove('btn-secondary');
            btn.classList.add('btn-primary');
            this.refreshInterval = setInterval(() => this.loadLogs(), 3000);
            notifications.info('Auto refresh enabled');
        } else {
            btn.classList.remove('btn-primary');
            btn.classList.add('btn-secondary');
            if (this.refreshInterval) {
                clearInterval(this.refreshInterval);
                this.refreshInterval = null;
            }
            notifications.info('Auto refresh disabled');
        }
    }

    async clearLogs() {
        if (!confirm('Are you sure you want to clear all traffic logs?')) {
            return;
        }

        try {
            await api.clearTrafficLogs();
            this.logs = [];
            this.renderLogs();
            notifications.success('Traffic logs cleared');
        } catch (error) {
            notifications.error('Failed to clear logs: ' + error.message);
        }
    }

    destroy() {
        if (this.refreshInterval) {
            clearInterval(this.refreshInterval);
        }
    }
}

export const traffic = new Traffic();
