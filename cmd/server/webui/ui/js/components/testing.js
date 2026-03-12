import { api } from '../api.js';
import { notifications } from '../utils/notifications.js';

class Testing {
    constructor() {
        this.container = document.getElementById('view-container');
        this.endpoints = [];
    }

    async render() {
        this.container.innerHTML = `
            <div class="testing">
                <h1>Endpoint Testing</h1>

                <div class="card mt-3">
                    <div class="card-body">
                        <div class="form-group">
                            <label class="form-label">Select Endpoint</label>
                            <select class="form-select" id="test-endpoint-select">
                                <option value="">Loading...</option>
                            </select>
                        </div>

                        <div class="form-group">
                            <button class="btn btn-primary" id="test-btn">Run Test</button>
                        </div>

                        <div id="test-result" class="mt-3" style="display: none;"></div>
                    </div>
                </div>
            </div>
        `;

        document.getElementById('test-btn').addEventListener('click', () => this.runTest());

        await this.loadEndpoints();
    }

    async loadEndpoints() {
        try {
            const data = await api.getEndpoints();
            this.endpoints = data.endpoints || [];

            const select = document.getElementById('test-endpoint-select');
            const enabledEndpoints = this.endpoints.filter(ep => ep.enabled);

            if (enabledEndpoints.length === 0) {
                select.innerHTML = '<option value="">No enabled endpoints</option>';
                return;
            }

            select.innerHTML = enabledEndpoints.map(ep =>
                `<option value="${this.escapeHtml(ep.name)}">${this.escapeHtml(ep.name)}</option>`
            ).join('');
        } catch (error) {
            notifications.error('Failed to load endpoints: ' + error.message);
        }
    }

    async runTest() {
        const select = document.getElementById('test-endpoint-select');
        const endpointName = select.value;

        if (!endpointName) {
            notifications.warning('Please select an endpoint');
            return;
        }

        const resultDiv = document.getElementById('test-result');
        resultDiv.style.display = 'block';
        resultDiv.innerHTML = '<div class="flex-center"><div class="spinner"></div></div>';

        try {
            const result = await api.testEndpoint(endpointName);

            if (result.success) {
                const responseText = result.response || 'No response';
                resultDiv.innerHTML = `
                    <div class="card testing-result-card">
                        <div class="testing-result-header">
                            <div>
                                <span class="badge badge-success">Success</span>
                                <span class="text-muted ml-2">Latency: ${result.latency}ms</span>
                            </div>
                            <button class="btn btn-sm btn-secondary testing-copy-btn">
                                Copy response
                            </button>
                        </div>
                        <div>
                            <pre class="code-block testing-result-code-block">${this.formatJSON(responseText)}</pre>
                        </div>
                    </div>
                `;
                const copyBtn = document.querySelector('.testing-copy-btn');
                if (copyBtn) {
                    copyBtn.addEventListener('click', () => {
                        this.copyText(responseText);
                    });
                }
                notifications.success('Test completed successfully');
            } else {
                const errorText = result.error || 'Unknown error';
                resultDiv.innerHTML = `
                    <div class="card testing-result-card">
                        <div class="testing-result-header">
                            <div>
                                <span class="badge badge-danger">Failed</span>
                            </div>
                            <button class="btn btn-sm btn-secondary testing-copy-btn">
                                Copy error
                            </button>
                        </div>
                        <div>
                            <pre class="code-block testing-result-code-block">${this.formatJSON(errorText)}</pre>
                        </div>
                    </div>
                `;
                const copyBtn = document.querySelector('.testing-copy-btn');
                if (copyBtn) {
                    copyBtn.addEventListener('click', () => {
                        this.copyText(errorText);
                    });
                }
                notifications.error('Test failed');
            }
        } catch (error) {
            const errorText = error.message || 'Unknown error';
            resultDiv.innerHTML = `
                <div class="card testing-result-card">
                    <div class="testing-result-header">
                        <div>
                            <span class="badge badge-danger">Error</span>
                        </div>
                        <button class="btn btn-sm btn-secondary testing-copy-btn">
                            Copy error
                        </button>
                    </div>
                    <div>
                        <pre class="code-block testing-result-code-block">${this.formatJSON(errorText)}</pre>
                    </div>
                </div>
            `;
            const copyBtn = document.querySelector('.testing-copy-btn');
            if (copyBtn) {
                copyBtn.addEventListener('click', () => {
                    this.copyText(errorText);
                });
            }
            notifications.error('Test failed: ' + error.message);
        }
    }

    formatJSON(str) {
        if (!str) return this.escapeHtml('No response');
        try {
            const obj = JSON.parse(str);
            return this.escapeHtml(JSON.stringify(obj, null, 2));
        } catch {
            return this.escapeHtml(str);
        }
    }

    copyText(text) {
        if (!text) {
            notifications.error('Nothing to copy');
            return;
        }

        try {
            if (typeof navigator !== 'undefined' &&
                navigator.clipboard &&
                typeof navigator.clipboard.writeText === 'function') {
                navigator.clipboard.writeText(text).then(() => {
                    notifications.success('Copied to clipboard');
                }).catch(() => {
                    notifications.error('Failed to copy to clipboard');
                });
                return;
            }
        } catch {
            // fall through
        }

        try {
            const textarea = document.createElement('textarea');
            textarea.value = text;
            textarea.style.position = 'fixed';
            textarea.style.top = '-1000px';
            textarea.style.left = '-1000px';
            document.body.appendChild(textarea);
            textarea.focus();
            textarea.select();
            const ok = document.execCommand && document.execCommand('copy');
            document.body.removeChild(textarea);
            if (ok) {
                notifications.success('Copied to clipboard');
            } else {
                notifications.error('Failed to copy to clipboard');
            }
        } catch {
            notifications.error('Failed to copy to clipboard');
        }
    }
    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
}

export const testing = new Testing();
