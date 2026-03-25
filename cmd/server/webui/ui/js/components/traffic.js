import { api } from '../api.js';
import { notifications } from '../utils/notifications.js';
import { escapeHtml, formatDateTime, formatLatency, formatTokens } from '../utils/formatters.js';

class Traffic {
    constructor() {
        this.container = document.getElementById('view-container');
        this.modalContainer = document.getElementById('modal-container');
        this.loadVersion = 0;
    }

    async render() {
        this.container.innerHTML = `
            <div class="traffic">
                <div class="page-header">
                    <div>
                        <h1>Traffic Logs</h1>
                        <p class="page-subtitle">Inspect raw and transformed requests, responses, and SSE streams for recent proxy calls.</p>
                    </div>
                    <div class="toolbar-actions">
                        <button class="btn btn-secondary" id="traffic-refresh-btn">Refresh</button>
                        <button class="btn btn-danger" id="traffic-clear-btn">Clear Logs</button>
                    </div>
                </div>

                <div class="card mt-3">
                    <div class="card-body">
                        <div class="toolbar-grid">
                            <div class="form-group">
                                <label class="form-label">Endpoint</label>
                                <select class="form-select" id="traffic-endpoint-filter">
                                    <option value="">All endpoints</option>
                                </select>
                            </div>
                            <div class="form-group">
                                <label class="form-label">Client Format</label>
                                <select class="form-select" id="traffic-format-filter">
                                    <option value="">All formats</option>
                                    <option value="claude">Claude</option>
                                    <option value="openai">OpenAI</option>
                                    <option value="openai2">Responses</option>
                                    <option value="gemini">Gemini</option>
                                </select>
                            </div>
                            <div class="form-group">
                                <label class="form-label">Errors</label>
                                <select class="form-select" id="traffic-error-filter">
                                    <option value="">All</option>
                                    <option value="true">Only errors</option>
                                    <option value="false">Only success</option>
                                </select>
                            </div>
                            <div class="form-group">
                                <label class="form-label">Limit</label>
                                <select class="form-select" id="traffic-limit-filter">
                                    <option value="10">10</option>
                                    <option value="5">5</option>
                                    <option value="3">3</option>
                                </select>
                            </div>
                        </div>

                        <div class="toolbar-grid compact">
                            <div class="switch-row">
                                <label class="switch-label" for="traffic-recording-toggle">Record traffic in memory</label>
                                <label class="toggle-switch server-toggle">
                                    <input type="checkbox" id="traffic-recording-toggle">
                                    <span class="toggle-slider"></span>
                                </label>
                            </div>
                            <div class="traffic-summary" id="traffic-summary">Loading...</div>
                        </div>
                    </div>
                </div>

                <div class="card mt-3">
                    <div class="card-body">
                        <div id="traffic-table-container" class="table-container"></div>
                    </div>
                </div>
            </div>
        `;

        this.attachEvents();
        await Promise.all([this.loadEndpoints(), this.loadTraffic()]);
    }

    attachEvents() {
        document.getElementById('traffic-refresh-btn').addEventListener('click', () => this.loadTraffic());
        document.getElementById('traffic-clear-btn').addEventListener('click', () => this.clearTraffic());
        document.getElementById('traffic-endpoint-filter').addEventListener('change', () => this.loadTraffic());
        document.getElementById('traffic-format-filter').addEventListener('change', () => this.loadTraffic());
        document.getElementById('traffic-error-filter').addEventListener('change', () => this.loadTraffic());
        document.getElementById('traffic-limit-filter').addEventListener('change', () => this.loadTraffic());
        document.getElementById('traffic-recording-toggle').addEventListener('change', (event) => this.setRecording(event.target.checked));
    }

    async loadEndpoints() {
        try {
            const data = await api.getEndpoints();
            const select = document.getElementById('traffic-endpoint-filter');
            const options = (data.endpoints || []).map((endpoint) =>
                `<option value="${escapeHtml(endpoint.name)}">${escapeHtml(endpoint.name)}</option>`
            );
            select.innerHTML = `<option value="">All endpoints</option>${options.join('')}`;
        } catch (error) {
            notifications.error('Failed to load endpoints: ' + error.message);
        }
    }

    buildFilter() {
        return {
            endpointName: document.getElementById('traffic-endpoint-filter').value,
            clientFormat: document.getElementById('traffic-format-filter').value,
            hasError: document.getElementById('traffic-error-filter').value,
            limit: document.getElementById('traffic-limit-filter').value
        };
    }

    async loadTraffic() {
        const loadVersion = ++this.loadVersion;
        try {
            const data = await api.getTraffic(this.buildFilter());
            if (loadVersion !== this.loadVersion) {
                return;
            }
            const toggle = document.getElementById('traffic-recording-toggle');
            toggle.checked = Boolean(data.recording);
            document.getElementById('traffic-summary').textContent = `${data.count || 0} shown / ${data.total || 0} buffered`;
            this.renderTable(data.logs || []);
        } catch (error) {
            if (loadVersion !== this.loadVersion) {
                return;
            }
            notifications.error('Failed to load traffic logs: ' + error.message);
        }
    }

    renderTable(logs) {
        const container = document.getElementById('traffic-table-container');
        if (!logs.length) {
            container.innerHTML = `<div class="empty-state"><p>No traffic logs captured yet.</p></div>`;
            return;
        }

        container.innerHTML = `
            <table class="table">
                <thead>
                    <tr>
                        <th>Time</th>
                        <th>Endpoint</th>
                        <th>Client</th>
                        <th>Event</th>
                        <th>Status</th>
                        <th>Latency</th>
                        <th>Tokens</th>
                        <th>Mode</th>
                        <th></th>
                    </tr>
                </thead>
                <tbody>
                    ${logs.map((log) => `
                        <tr>
                            <td>${escapeHtml(formatDateTime(log.timestamp))}</td>
                            <td>${escapeHtml(log.endpointName || '-')}</td>
                            <td>${escapeHtml(log.clientFormat || '-')}</td>
                            <td><code>${escapeHtml(log.eventType || '-')}</code></td>
                            <td>${this.renderStatus(log)}</td>
                            <td>${escapeHtml(formatLatency(log.duration || 0))}</td>
                            <td>${escapeHtml(`${formatTokens(log.inputTokens || 0)} / ${formatTokens(log.outputTokens || 0)}`)}</td>
                            <td>${log.isStreaming ? 'SSE' : 'JSON'}</td>
                            <td><button class="btn btn-secondary btn-sm traffic-detail-btn" data-id="${escapeHtml(log.id)}">Inspect</button></td>
                        </tr>
                    `).join('')}
                </tbody>
            </table>
        `;

        container.querySelectorAll('.traffic-detail-btn').forEach((button) => {
            button.addEventListener('click', () => this.showDetail(button.dataset.id));
        });
    }

    renderStatus(log) {
        if (log.error) {
            return `<span class="badge badge-danger">${escapeHtml(log.statusCode ? String(log.statusCode) : 'Error')}</span>`;
        }
        if ((log.statusCode || 0) >= 400) {
            return `<span class="badge badge-warning">${escapeHtml(String(log.statusCode))}</span>`;
        }
        if (log.statusCode) {
            return `<span class="badge badge-success">${escapeHtml(String(log.statusCode))}</span>`;
        }
        return '<span class="badge badge-info">Pending</span>';
    }

    async setRecording(enabled) {
        try {
            await api.setTrafficRecording(enabled);
            notifications.success(enabled ? 'Traffic recording enabled' : 'Traffic recording disabled');
            await this.loadTraffic();
        } catch (error) {
            document.getElementById('traffic-recording-toggle').checked = !enabled;
            notifications.error('Failed to update recording: ' + error.message);
        }
    }

    async clearTraffic() {
        if (!window.confirm('Clear all buffered traffic logs?')) {
            return;
        }

        try {
            this.loadVersion += 1;
            await api.clearTraffic();
            document.getElementById('traffic-summary').textContent = '0 shown / 0 buffered';
            this.renderTable([]);
            notifications.success('Traffic logs cleared');
            await this.loadTraffic();
        } catch (error) {
            notifications.error('Failed to clear traffic logs: ' + error.message);
        }
    }

    async showDetail(id) {
        try {
            const detail = await api.getTrafficDetail(id);
            this.modalContainer.innerHTML = `
                <div class="modal-overlay" id="traffic-detail-modal">
                    <div class="modal modal-lg">
                        <div class="modal-header">
                            <h2 class="modal-title">Traffic Detail</h2>
                            <button class="modal-close" id="traffic-detail-close">×</button>
                        </div>
                        <div class="modal-body">
                            <div class="detail-grid">
                                <div class="detail-card"><span class="detail-label">Request ID</span><span class="detail-value">${escapeHtml(detail.requestId || '-')}</span></div>
                                <div class="detail-card"><span class="detail-label">Endpoint</span><span class="detail-value">${escapeHtml(detail.endpointName || '-')}</span></div>
                                <div class="detail-card"><span class="detail-label">Path</span><span class="detail-value">${escapeHtml(detail.path || '-')}</span></div>
                                <div class="detail-card"><span class="detail-label">Status</span><span class="detail-value">${escapeHtml(detail.statusCode ? String(detail.statusCode) : '-')}</span></div>
                            </div>
                            <div class="callout mt-3">
                                <strong>Event:</strong> ${escapeHtml(detail.eventType || '-')}
                                <span class="callout-divider">|</span>
                                <strong>Streaming:</strong> ${detail.isStreaming ? 'Yes' : 'No'}
                                <span class="callout-divider">|</span>
                                <strong>Error:</strong> ${escapeHtml(detail.error || 'None')}
                            </div>
                            <div class="code-grid mt-3">
                                ${this.renderCodePanel('Original Request', detail.originalRequest)}
                                ${this.renderCodePanel('Transformed Request', detail.transformedRequest)}
                                ${this.renderCodePanel('Original Response / SSE', detail.originalResponse)}
                                ${this.renderCodePanel('Transformed Response / SSE', detail.transformedResponse)}
                            </div>
                        </div>
                    </div>
                </div>
            `;

            document.getElementById('traffic-detail-close').addEventListener('click', () => this.closeDetail());
            document.getElementById('traffic-detail-modal').addEventListener('click', (event) => {
                if (event.target.id === 'traffic-detail-modal') {
                    this.closeDetail();
                }
            });
        } catch (error) {
            notifications.error('Failed to load traffic detail: ' + error.message);
        }
    }

    renderCodePanel(title, value) {
        return `
            <div class="code-panel">
                <div class="code-panel-header">${escapeHtml(title)}</div>
                <pre class="code-block">${escapeHtml(this.prettyPrint(value))}</pre>
            </div>
        `;
    }

    prettyPrint(value) {
        if (!value) {
            return '(empty)';
        }

        try {
            return JSON.stringify(JSON.parse(value), null, 2);
        } catch (_) {
            return value;
        }
    }

    closeDetail() {
        this.modalContainer.innerHTML = '';
    }
}

export const traffic = new Traffic();
