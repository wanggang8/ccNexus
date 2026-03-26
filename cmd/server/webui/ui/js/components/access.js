import { api } from '../api.js';
import { notifications } from '../utils/notifications.js';
import { escapeHtml } from '../utils/formatters.js';

class Access {
    constructor() {
        this.container = document.getElementById('view-container');
    }

    async render() {
        this.container.innerHTML = `
            <div class="access">
                <div class="page-header">
                    <div>
                        <h1>Access Control</h1>
                        <p class="page-subtitle">Manage admin Basic Auth and the shared proxy token used to protect server-side API routes.</p>
                    </div>
                </div>

                <div class="grid grid-cols-2 mt-3">
                    <div class="card">
                        <div class="card-header">
                            <h3 class="card-title">Admin UI and API</h3>
                        </div>
                        <div class="card-body">
                            <div class="form-group">
                                <div class="switch-row">
                                    <label class="switch-label" for="basic-auth-enabled">Enable Basic Auth</label>
                                    <label class="toggle-switch server-toggle">
                                        <input type="checkbox" id="basic-auth-enabled">
                                        <span class="toggle-slider"></span>
                                    </label>
                                </div>
                            </div>
                            <div class="form-group">
                                <label class="form-label">Username</label>
                                <input class="form-input" id="basic-auth-username" placeholder="admin">
                            </div>
                            <div class="form-group">
                                <label class="form-label">Password</label>
                                <input class="form-input" id="basic-auth-password" type="password" placeholder="Leave unchanged to keep current password">
                            </div>
                            <div class="toolbar-actions">
                                <button class="btn btn-primary" id="save-access-btn">Save</button>
                                <button class="btn btn-secondary" id="reset-password-btn">Generate New Password</button>
                            </div>
                        </div>
                    </div>

                    <div class="card">
                        <div class="card-header">
                            <h3 class="card-title">Proxy Token for /v1/*</h3>
                        </div>
                        <div class="card-body">
                            <div class="callout">
                                When Basic Auth is enabled, the same password also protects server proxy routes. Clients can access protected routes with:
                                <div class="callout-code mt-2">Authorization: Bearer &lt;password&gt;</div>
                                <div class="callout-code mt-1">X-API-Token: &lt;password&gt;</div>
                            </div>
                            <div class="detail-grid mt-3">
                                <div class="detail-card">
                                    <span class="detail-label">Protected paths</span>
                                    <span class="detail-value">/v1/*, /chat/completions, /responses, /cursor/*</span>
                                </div>
                                <div class="detail-card">
                                    <span class="detail-label">Current token source</span>
                                    <span class="detail-value" id="proxy-token-source">Basic Auth password</span>
                                </div>
                            </div>
                            <div class="toolbar-actions mt-3">
                                <button class="btn btn-secondary" id="copy-proxy-token-btn">Copy Current Password</button>
                            </div>
                            <p class="helper-text mt-2">The password is never returned in plain text on read. Generate a new one here if you need to rotate both the admin login and the proxy token together.</p>
                        </div>
                    </div>
                </div>
            </div>
        `;

        this.attachEvents();
        await this.loadConfig();
    }

    attachEvents() {
        document.getElementById('save-access-btn').addEventListener('click', () => this.save());
        document.getElementById('reset-password-btn').addEventListener('click', () => this.resetPassword());
        document.getElementById('copy-proxy-token-btn').addEventListener('click', () => this.copyPassword());
    }

    async loadConfig() {
        try {
            const data = await api.getBasicAuthConfig();
            document.getElementById('basic-auth-enabled').checked = Boolean(data.enabled);
            document.getElementById('basic-auth-username').value = data.username || '';
            document.getElementById('basic-auth-password').value = data.password || '';
        } catch (error) {
            notifications.error('Failed to load access settings: ' + error.message);
        }
    }

    async save() {
        try {
            const payload = {
                enabled: document.getElementById('basic-auth-enabled').checked,
                username: document.getElementById('basic-auth-username').value.trim(),
                password: document.getElementById('basic-auth-password').value
            };

            await api.updateBasicAuthConfig(payload);
            notifications.success('Access settings saved');
            await this.loadConfig();
        } catch (error) {
            notifications.error('Failed to save access settings: ' + error.message);
        }
    }

    async resetPassword() {
        try {
            const data = await api.resetBasicAuthPassword();
            document.getElementById('basic-auth-password').value = data.password || '';
            notifications.success('Password rotated. Save if you want to update username/enabled state too.');
        } catch (error) {
            notifications.error('Failed to reset password: ' + error.message);
        }
    }

    async copyPassword() {
        try {
            let password = document.getElementById('basic-auth-password').value;
            if (!password || password === '***') {
                const data = await api.revealBasicAuthPassword();
                password = data.password || '';
            }
            if (!password) {
                notifications.warning('No password is currently set.');
                return;
            }
            await navigator.clipboard.writeText(password);
            document.getElementById('basic-auth-password').value = password;
            notifications.success('Password copied to clipboard');
        } catch (error) {
            notifications.error('Failed to copy password: ' + escapeHtml(error.message));
        }
    }
}

export const access = new Access();
