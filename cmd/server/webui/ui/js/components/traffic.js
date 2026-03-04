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

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    async render() {
        this.container.innerHTML = `
            <div class="traffic">
                <div class="flex justify-between align-center mb-4">
                    <div>
                        <h1 class="mb-1">Traffic Logs</h1>
                        <p class="text-sm text-muted">Monitor and analyze API traffic in real-time</p>
                    </div>
                    <div class="flex gap-2">
                        <button id="auto-refresh-btn" class="btn btn-sm btn-secondary" title="Auto refresh every 5s">
                            <span class="icon">${getIcon('refresh')}</span>
                            <span>Auto</span>
                        </button>
                        <button id="refresh-btn" class="btn btn-sm btn-secondary" title="Refresh now">
                            <span class="icon">${getIcon('refresh')}</span>
                        </button>
                        <button id="clear-btn" class="btn btn-sm btn-danger" title="Clear all logs">
                            <span class="icon">${getIcon('trash')}</span>
                            <span>Clear</span>
                        </button>
                        <button id="recording-toggle" class="btn btn-sm btn-secondary">
                            <span class="icon">${getIcon('activity')}</span>
                            <span id="recording-text">Start Recording</span>
                        </button>
                    </div>
                </div>

                <!-- Stats Cards -->
                <div id="traffic-stats" class="mb-4"></div>

                <!-- Filters -->
                <div class="card mb-4">
                    <div class="card-body">
                        <div class="flex gap-3 align-center flex-wrap">
                            <span class="text-sm font-semibold text-muted">Filters:</span>
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
                                <option value="">All Requests</option>
                                <option value="true">Errors Only</option>
                                <option value="false">Success Only</option>
                            </select>
                            <button id="reset-filter-btn" class="btn btn-sm btn-secondary">
                                <span class="icon">${getIcon('x')}</span>
                                Reset
                            </button>
                        </div>
                    </div>
                </div>

                <!-- Logs List -->
                <div class="card">
                    <div class="card-body">
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
            notifications.error('Failed to load endpoints: ' + error.message);
        }
    }

    async loadLogs() {
        try {
            const data = await api.getTrafficLogs(this.filter);
            this.logs = data.logs || [];
            this.recording = data.recording !== undefined ? data.recording : false;
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
        if (!statsEl) return;
        const total = data.total !== undefined ? data.total : 0;
        const count = data.count !== undefined ? data.count : 0;
        const filtered = Math.max(0, total - count);
        
        statsEl.innerHTML = `
            <div class="stats-grid">
                <div class="stat-card">
                    <div class="stat-icon" style="background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);">
                        <span class="icon">${getIcon('activity')}</span>
                    </div>
                    <div class="stat-content">
                        <div class="stat-label">Total Requests</div>
                        <div class="stat-value">${total}</div>
                    </div>
                </div>
                
                <div class="stat-card">
                    <div class="stat-icon" style="background: linear-gradient(135deg, #f093fb 0%, #f5576c 100%);">
                        <span class="icon">${getIcon('filter')}</span>
                    </div>
                    <div class="stat-content">
                        <div class="stat-label">Filtered Results</div>
                        <div class="stat-value">${count}</div>
                    </div>
                </div>
                
                <div class="stat-card">
                    <div class="stat-icon" style="background: linear-gradient(135deg, #4facfe 0%, #00f2fe 100%);">
                        <span class="icon">${getIcon('eye-off')}</span>
                    </div>
                    <div class="stat-content">
                        <div class="stat-label">Hidden</div>
                        <div class="stat-value">${filtered}</div>
                    </div>
                </div>
                
                <div class="stat-card ${this.recording ? 'recording-active' : ''}">
                    <div class="stat-icon" style="background: ${this.recording ? 'linear-gradient(135deg, #fa709a 0%, #fee140 100%)' : 'linear-gradient(135deg, #a8edea 0%, #fed6e3 100%)'};">
                        <span class="icon">${this.recording ? getIcon('circle') : getIcon('circle')}</span>
                    </div>
                    <div class="stat-content">
                        <div class="stat-label">Recording Status</div>
                        <div class="stat-value" style="font-size: 1.25rem; font-weight: 600;">
                            ${this.recording ? 'Active' : 'Inactive'}
                        </div>
                    </div>
                </div>
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

        // Method badge color
        const methodColors = {
            'GET': 'badge-info',
            'POST': 'badge-success',
            'PUT': 'badge-warning',
            'DELETE': 'badge-danger',
            'PATCH': 'badge-primary'
        };
        const methodClass = methodColors[log.method] || 'badge-primary';

        return `
            <div class="traffic-log-item" data-id="${log.id}">
                <div class="traffic-log-header">
                    <span class="traffic-log-time">
                        <span class="icon" style="font-size: 0.875rem;">${getIcon('clock')}</span>
                        ${timestamp}
                    </span>
                    <span class="badge ${methodClass}">${log.method}</span>
                    <span class="traffic-log-endpoint">${log.endpointName}</span>
                    <span class="traffic-log-format">${log.clientFormat}</span>
                    <span class="traffic-log-status ${statusClass}">${log.statusCode}</span>
                    <span class="traffic-log-duration">
                        <span class="icon" style="font-size: 0.75rem;">${getIcon('zap')}</span>
                        ${duration}
                    </span>
                    ${streamBadge}
                    ${truncatedBadge}
                    ${errorBadge}
                </div>
                <div class="traffic-log-details">
                    <span class="traffic-log-path">
                        <span class="icon" style="font-size: 0.875rem;">${getIcon('link')}</span>
                        ${log.path}
                    </span>
                    ${log.transformerName ? `
                        <span class="text-muted">
                            <span class="icon" style="font-size: 0.875rem;">${getIcon('arrow-right')}</span>
                            ${log.transformerName}
                        </span>
                    ` : ''}
                    ${log.inputTokens || log.outputTokens ? `
                        <span class="text-muted">
                            <span class="icon" style="font-size: 0.875rem;">${getIcon('hash')}</span>
                            Tokens: ${log.inputTokens || 0}/${log.outputTokens || 0}
                        </span>
                    ` : ''}
                </div>
                ${log.error ? `
                    <div class="traffic-log-error">
                        <span class="icon" style="font-size: 0.875rem;">${getIcon('alert-circle')}</span>
                        ${this.escapeHtml(log.error)}
                    </div>
                ` : ''}
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

        // Build tabs array
        const tabs = [
            { id: 'original-request', label: 'Original Request', content: detail.originalRequest }
        ];
        
        if (detail.transformedRequest) {
            tabs.push({ id: 'transformed-request', label: 'Transformed Request', content: detail.transformedRequest });
        }
        
        tabs.push({ id: 'original-response', label: 'Original Response', content: detail.originalResponse });
        
        if (detail.transformedResponse) {
            tabs.push({ id: 'transformed-response', label: 'Transformed Response', content: detail.transformedResponse });
        }

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
                                <h3>Request & Response</h3>
                                <div class="tabs">
                                    <div class="tab-list" role="tablist">
                                        ${tabs.map((tab, index) => `
                                            <button 
                                                class="tab-button ${index === 0 ? 'active' : ''}" 
                                                data-tab="${tab.id}"
                                                role="tab"
                                                aria-selected="${index === 0}"
                                            >
                                                ${tab.label}
                                            </button>
                                        `).join('')}
                                    </div>
                                    <div class="tab-content">
                                        ${tabs.map((tab, index) => `
                                            <div 
                                                class="tab-panel ${index === 0 ? 'active' : ''}" 
                                                id="${tab.id}"
                                                role="tabpanel"
                                            >
                                                <pre class="code-block">${this.formatJSON(tab.content)}</pre>
                                            </div>
                                        `).join('')}
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        `;

        // Setup tab switching
        const tabButtons = modal.querySelectorAll('.tab-button');
        const tabPanels = modal.querySelectorAll('.tab-panel');
        
        tabButtons.forEach(button => {
            button.addEventListener('click', () => {
                const targetTab = button.getAttribute('data-tab');
                
                // Update buttons
                tabButtons.forEach(btn => {
                    btn.classList.remove('active');
                    btn.setAttribute('aria-selected', 'false');
                });
                button.classList.add('active');
                button.setAttribute('aria-selected', 'true');
                
                // Update panels
                tabPanels.forEach(panel => {
                    panel.classList.remove('active');
                });
                document.getElementById(targetTab).classList.add('active');
            });
        });

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
            const result = await api.setTrafficRecording(newState);
            // Use server's confirmed state instead of assuming success
            this.recording = result.recording !== undefined ? result.recording : newState;
            this.updateRecordingButton();
            notifications.success(`Recording ${this.recording ? 'enabled' : 'disabled'}`);
            // Refresh logs to sync state and show any new logs
            await this.loadLogs();
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
