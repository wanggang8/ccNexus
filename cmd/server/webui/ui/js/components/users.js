import { api } from '../api.js';
import { notifications } from '../utils/notifications.js';
import { escapeHtml, formatDateTime, copyText } from '../utils/formatters.js';
import { getIcon } from '../icons.js';

class Users {
    constructor() {
        this.container = document.getElementById('view-container');
        this.users = [];
    }

    async render() {
        this.container.innerHTML = `
            <div class="users-view">
                <div class="flex-between mb-3">
                    <h1>Users</h1>
                    <button class="btn btn-primary" id="add-user-btn">
                        <span>+ Add User</span>
                    </button>
                </div>
                <div class="card">
                    <div class="card-body">
                        <div id="users-table"></div>
                    </div>
                </div>
            </div>
        `;

        document.getElementById('add-user-btn').addEventListener('click', () => this.showCreateUserModal());
        await this.loadUsers();
    }

    async loadUsers() {
        try {
            const data = await api.getUsers();
            this.users = data.users || [];
            this.renderTable();
        } catch (error) {
            notifications.error('Failed to load users: ' + error.message);
            const container = document.getElementById('users-table');
            if (container) {
                container.innerHTML = `<div class="empty-state"><p>${escapeHtml(error.message)}</p></div>`;
            }
        }
    }

    renderTable() {
        const container = document.getElementById('users-table');
        if (!container) return;

        if (!this.users.length) {
            container.innerHTML = `
                <div class="empty-state">
                    <div class="empty-state-icon"><span class="icon icon-2xl">${getIcon('users')}</span></div>
                    <div class="empty-state-title">No Users</div>
                    <div class="empty-state-message">Create your first managed user</div>
                </div>
            `;
            return;
        }

        container.innerHTML = `
            <div class="table-container">
                <table class="table">
                    <thead>
                        <tr>
                            <th>ID</th>
                            <th>Username</th>
                            <th>Role</th>
                            <th>Status</th>
                            <th>Current Endpoint</th>
                            <th>Last Used</th>
                            <th>Created</th>
                            <th>Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${this.users.map(user => `
                            <tr>
                                <td>${user.id}</td>
                                <td><strong>${escapeHtml(user.username)}</strong>${user.id === 1 ? ' <span class="badge badge-primary">Default Admin</span>' : ''}</td>
                                <td><span class="badge ${user.role === 'admin' ? 'badge-info' : 'badge-primary'}">${escapeHtml(user.role)}</span></td>
                                <td>${this.renderStatusBadge(user.status)}</td>
                                <td>${escapeHtml(user.currentEndpointName || '-')}</td>
                                <td>${user.lastUsedAt ? escapeHtml(formatDateTime(user.lastUsedAt)) : '-'}</td>
                                <td>${user.createdAt ? escapeHtml(formatDateTime(user.createdAt)) : '-'}</td>
                                <td>
                                    <div class="flex gap-2 user-actions">
                                        <button class="btn btn-sm btn-secondary reset-user-token-btn" data-id="${user.id}" data-name="${escapeHtml(user.username)}">Reset Token</button>
                                        ${user.status === 'active'
                                            ? `<button class="btn btn-sm btn-danger toggle-user-status-btn" data-id="${user.id}" data-status="disabled" data-name="${escapeHtml(user.username)}">Disable</button>`
                                            : `<button class="btn btn-sm btn-secondary toggle-user-status-btn" data-id="${user.id}" data-status="active" data-name="${escapeHtml(user.username)}">Enable</button>`}
                                    </div>
                                </td>
                            </tr>
                        `).join('')}
                    </tbody>
                </table>
            </div>
        `;

        container.querySelectorAll('.reset-user-token-btn').forEach(btn => {
            btn.addEventListener('click', () => this.resetUserToken(Number(btn.dataset.id), btn.dataset.name));
        });
        container.querySelectorAll('.toggle-user-status-btn').forEach(btn => {
            btn.addEventListener('click', () => this.toggleUserStatus(Number(btn.dataset.id), btn.dataset.status, btn.dataset.name));
        });
    }

    renderStatusBadge(status) {
        if (status === 'active') {
            return '<span class="badge badge-success">Active</span>';
        }
        return '<span class="badge badge-danger">Disabled</span>';
    }

    showCreateUserModal() {
        const modalContainer = document.getElementById('modal-container');
        modalContainer.innerHTML = `
            <div class="modal-overlay">
                <div class="modal">
                    <div class="modal-header">
                        <h3 class="modal-title">Create User</h3>
                        <button class="modal-close" id="close-user-modal">&times;</button>
                    </div>
                    <div class="modal-body">
                        <div class="form-group">
                            <label class="form-label">Username</label>
                            <input type="text" class="form-input" id="new-user-username" placeholder="Enter username">
                        </div>
                        <div class="form-group">
                            <label class="form-label">Role</label>
                            <select class="form-select" id="new-user-role">
                                <option value="user">user</option>
                                <option value="admin">admin</option>
                            </select>
                        </div>
                        <p class="text-muted text-sm">A token will be generated automatically and shown once after creation.</p>
                    </div>
                    <div class="modal-footer">
                        <button class="btn btn-secondary" id="cancel-user-modal">Cancel</button>
                        <button class="btn btn-primary" id="confirm-create-user">Create</button>
                    </div>
                </div>
            </div>
        `;

        modalContainer.querySelector('#close-user-modal').addEventListener('click', () => this.closeModal());
        modalContainer.querySelector('#cancel-user-modal').addEventListener('click', () => this.closeModal());
        modalContainer.querySelector('#confirm-create-user').addEventListener('click', () => this.createUser());
    }

    closeModal() {
        const modalContainer = document.getElementById('modal-container');
        if (modalContainer) {
            modalContainer.innerHTML = '';
        }
    }

    async createUser() {
        const username = document.getElementById('new-user-username').value.trim();
        const role = document.getElementById('new-user-role').value;
        if (!username) {
            notifications.error('Username is required');
            return;
        }
        try {
            const data = await api.createUser({ username, role });
            this.closeModal();
            this.showTokenModal(`User ${username} created`, data.token);
            await this.loadUsers();
        } catch (error) {
            notifications.error('Failed to create user: ' + error.message);
        }
    }

    async resetUserToken(id, username) {
        if (!confirm(`Reset token for ${username}? The old token will stop working immediately.`)) {
            return;
        }
        try {
            const data = await api.resetUserToken(id);
            this.showTokenModal(`New token for ${username}`, data.token);
            await this.loadUsers();
        } catch (error) {
            notifications.error('Failed to reset token: ' + error.message);
        }
    }

    async toggleUserStatus(id, status, username) {
        if (!confirm(`${status === 'disabled' ? 'Disable' : 'Enable'} user ${username}?`)) {
            return;
        }
        try {
            await api.updateUserStatus(id, status);
            notifications.success(`User ${username} ${status === 'disabled' ? 'disabled' : 'enabled'} successfully`);
            await this.loadUsers();
        } catch (error) {
            notifications.error('Failed to update user status: ' + error.message);
        }
    }

    showTokenModal(title, token) {
        const modalContainer = document.getElementById('modal-container');
        modalContainer.innerHTML = `
            <div class="modal-overlay">
                <div class="modal">
                    <div class="modal-header">
                        <h3 class="modal-title">${escapeHtml(title)}</h3>
                        <button class="modal-close" id="close-token-modal">&times;</button>
                    </div>
                    <div class="modal-body">
                        <div class="token-display-box">
                            <code id="generated-user-token">${escapeHtml(token)}</code>
                        </div>
                        <p class="text-muted text-sm mt-2">This token is shown only once. Save it now.</p>
                    </div>
                    <div class="modal-footer">
                        <button class="btn btn-secondary" id="copy-user-token-btn">Copy Token</button>
                        <button class="btn btn-primary" id="close-user-token-btn">Done</button>
                    </div>
                </div>
            </div>
        `;

        modalContainer.querySelector('#close-token-modal').addEventListener('click', () => this.closeModal());
        modalContainer.querySelector('#close-user-token-btn').addEventListener('click', () => this.closeModal());
        modalContainer.querySelector('#copy-user-token-btn').addEventListener('click', async () => {
            try {
                await copyText(token);
                notifications.success('Token copied to clipboard');
            } catch (error) {
                notifications.error('Failed to copy token');
            }
        });
    }
}

export const users = new Users();
