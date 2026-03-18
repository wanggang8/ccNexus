import { api } from '../api.js';
import { notifications } from '../utils/notifications.js';
import { formatNumber, formatTokens, escapeHtml } from '../utils/formatters.js';
import { getIcon } from '../icons.js';

class Stats {
    constructor() {
        this.container = document.getElementById('view-container');
    }

    async render() {
        this.container.innerHTML = `
            <div class="stats">
                <h1>Statistics</h1>

                <div class="flex gap-2 mt-3 mb-3">
                    <button class="btn btn-sm btn-primary period-btn active" data-period="daily">Daily</button>
                    <button class="btn btn-sm btn-secondary period-btn" data-period="weekly">Weekly</button>
                    <button class="btn btn-sm btn-secondary period-btn" data-period="monthly">Monthly</button>
                </div>

                <div id="stats-content"></div>
            </div>
        `;

        document.querySelectorAll('.period-btn').forEach(btn => {
            btn.addEventListener('click', () => {
                document.querySelectorAll('.period-btn').forEach(b => {
                    b.classList.remove('btn-primary', 'active');
                    b.classList.add('btn-secondary');
                });
                btn.classList.remove('btn-secondary');
                btn.classList.add('btn-primary', 'active');
                this.loadStats(btn.dataset.period);
            });
        });

        await this.loadStats('daily');
    }

    async loadStats(period) {
        try {
            let data;
            switch (period) {
                case 'daily':
                    data = await api.getStatsDaily();
                    break;
                case 'weekly':
                    data = await api.getStatsWeekly();
                    break;
                case 'monthly':
                    data = await api.getStatsMonthly();
                    break;
            }

            this.renderStats(data);
        } catch (error) {
            notifications.error('Failed to load statistics: ' + error.message);
        }
    }

    renderStats(data) {
        const stats = data.stats || {};
        const container = document.getElementById('stats-content');

        container.innerHTML = `
            <div class="grid grid-cols-4 mb-4">
                <div class="stat-card">
                    <div class="stat-icon" style="background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);">
                        <span class="icon">${getIcon('activity')}</span>
                    </div>
                    <div class="stat-content">
                        <div class="stat-label">Total Requests</div>
                        <div class="stat-value">${formatNumber(stats.totalRequests || 0)}</div>
                    </div>
                </div>
                <div class="stat-card">
                    <div class="stat-icon" style="background: linear-gradient(135deg, #10b981 0%, #059669 100%);">
                        <span class="icon">${getIcon('checkCircle')}</span>
                    </div>
                    <div class="stat-content">
                        <div class="stat-label">Successful</div>
                        <div class="stat-value">${formatNumber(stats.totalSuccess || 0)}</div>
                    </div>
                </div>
                <div class="stat-card">
                    <div class="stat-icon" style="background: linear-gradient(135deg, #ef4444 0%, #b91c1c 100%);">
                        <span class="icon">${getIcon('alert-circle')}</span>
                    </div>
                    <div class="stat-content">
                        <div class="stat-label">Errors</div>
                        <div class="stat-value">${formatNumber(stats.totalErrors || 0)}</div>
                    </div>
                </div>
                <div class="stat-card">
                    <div class="stat-icon" style="background: linear-gradient(135deg, #8b5cf6 0%, #6366f1 100%);">
                        <span class="icon">${getIcon('hash')}</span>
                    </div>
                    <div class="stat-content">
                        <div class="stat-label">Total Tokens</div>
                        <div class="stat-value">${formatTokens((stats.totalInputTokens || 0) + (stats.totalOutputTokens || 0))}</div>
                    </div>
                </div>
            </div>

            <div class="card">
                <div class="card-header">
                    <h3 class="card-title">Endpoint Breakdown</h3>
                </div>
                <div class="card-body">
                    ${this.renderEndpointTable(stats.endpoints || {})}
                </div>
            </div>
        `;
    }

    renderEndpointTable(endpoints) {
        const endpointNames = Object.keys(endpoints);

        if (endpointNames.length === 0) {
            return '<div class="empty-state"><p>No data available</p></div>';
        }

        return `
            <div class="table-container">
                <table class="table">
                    <thead>
                        <tr>
                            <th>Endpoint</th>
                            <th>Requests</th>
                            <th>Errors</th>
                            <th>Input Tokens</th>
                            <th>Output Tokens</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${endpointNames.map(name => {
                            const ep = endpoints[name];
                            const requests = ep.requests || 0;
                            const errors = ep.errors || 0;
                            const errorRate = requests > 0 ? errors / requests : 0;
                            const highError = errorRate >= 0.05 && errors >= 5;
                            const rowClass = highError ? 'stats-endpoint-row-high-error' : '';
                            return `
                                <tr class="${rowClass}">
                                    <td><strong>${escapeHtml(name)}</strong></td>
                                    <td>${formatNumber(requests)}</td>
                                    <td>${formatNumber(errors)}</td>
                                    <td>${formatTokens(ep.inputTokens || 0)}</td>
                                    <td>${formatTokens(ep.outputTokens || 0)}</td>
                                </tr>
                            `;
                        }).join('')}
                    </tbody>
                </table>
            </div>
        `;
    }

}

export const stats = new Stats();
