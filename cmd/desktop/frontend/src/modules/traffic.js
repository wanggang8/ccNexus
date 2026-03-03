import { t } from '../i18n/index.js';
import { getIcon } from '../icons.js';
import { showNotification } from './modal.js';

let refreshInterval = null;
let currentFilter = {};
let currentTrafficDetail = null; // Store for copy/expand/collapse

// Toggle traffic recording
export async function toggleTrafficRecording() {
    try {
        const isRecording = await window.go.main.App.IsTrafficRecording();
        await window.go.main.App.SetTrafficRecording(!isRecording);
        updateRecordingUI(!isRecording);
        
        if (!isRecording) {
            // Started recording, begin auto-refresh
            startAutoRefresh();
        } else {
            // Stopped recording
            stopAutoRefresh();
        }
    } catch (error) {
        console.error('Failed to toggle traffic recording:', error);
    }
}

// Update recording button UI
function updateRecordingUI(isRecording) {
    const btn = document.getElementById('trafficRecordBtn');
    
    if (btn) {
        btn.classList.toggle('recording', isRecording);
        btn.innerHTML = isRecording 
            ? `<span class="record-dot recording"></span> ${t('traffic.stopRecording')}`
            : `<span class="record-dot"></span> ${t('traffic.startRecording')}`;
    }
}

// Load traffic logs
export async function loadTrafficLogs(filter = {}) {
    try {
        currentFilter = filter;
        const filterJSON = Object.keys(filter).length > 0 ? JSON.stringify(filter) : '';
        const resultStr = await window.go.main.App.GetTrafficLogs(filterJSON);
        const result = JSON.parse(resultStr);
        
        updateRecordingUI(result.recording);
        renderTrafficLogs(result.logs || []);
        updateTrafficCount(result.count, result.total);
        
        return result;
    } catch (error) {
        console.error('Failed to load traffic logs:', error);
        return null;
    }
}

// Render traffic logs list
function renderTrafficLogs(logs) {
    const container = document.getElementById('trafficLogList');
    if (!container) return;
    
    if (logs.length === 0) {
        container.innerHTML = `<div class="traffic-empty">${t('traffic.noLogs')}</div>`;
        return;
    }
    
    const html = logs.map(log => {
        const statusClass = log.statusCode >= 400 || log.error ? 'error' : 'success';
        const time = new Date(log.timestamp).toLocaleTimeString();
        const duration = log.duration < 1000 ? `${log.duration}ms` : `${(log.duration / 1000).toFixed(2)}s`;
        
        return `
            <div class="traffic-log-item ${statusClass}" onclick="window.showTrafficDetail('${escapeHtml(log.id)}')">
                <div class="traffic-log-status">
                    <span class="status-badge ${statusClass}">${log.statusCode || 'ERR'}</span>
                </div>
                <div class="traffic-log-info">
                    <div class="traffic-log-path">${escapeHtml(log.method)} ${escapeHtml(log.path)}</div>
                    <div class="traffic-log-meta">
                        <span class="traffic-endpoint">${escapeHtml(log.endpointName)}</span>
                        <span class="traffic-format">${escapeHtml(log.clientFormat)}</span>
                        ${log.isStreaming ? '<span class="traffic-streaming">SSE</span>' : ''}
                    </div>
                </div>
                <div class="traffic-log-tokens">
                    ${log.inputTokens || log.outputTokens ? `
                        <span class="token-in">${formatTokens(log.inputTokens)}</span>
                        <span class="token-sep">/</span>
                        <span class="token-out">${formatTokens(log.outputTokens)}</span>
                    ` : '-'}
                </div>
                <div class="traffic-log-duration">${escapeHtml(duration)}</div>
                <div class="traffic-log-time">${escapeHtml(time)}</div>
            </div>
        `;
    }).join('');
    
    container.innerHTML = html;
}

// Format token count
function formatTokens(count) {
    if (!count) return '0';
    if (count >= 1000000) return (count / 1000000).toFixed(1) + 'M';
    if (count >= 1000) return (count / 1000).toFixed(1) + 'K';
    return count.toString();
}

// Update traffic count display
function updateTrafficCount(count, total) {
    const countEl = document.getElementById('trafficCount');
    if (countEl) {
        countEl.textContent = `${count} / ${total}`;
    }
    
    // Update Tab badge
    const badge = document.getElementById('trafficTabBadge');
    if (badge) {
        badge.textContent = total > 0 ? total : '';
    }
}

// Show traffic detail modal
export async function showTrafficDetail(id) {
    try {
        const detailStr = await window.go.main.App.GetTrafficLogDetail(id);
        const detail = JSON.parse(detailStr);
        
        if (detail.error) {
            console.error('Traffic log not found');
            return;
        }
        
        renderTrafficDetailModal(detail);
        const modal = document.getElementById('trafficDetailModal');
        if (modal) modal.style.display = 'flex';
    } catch (error) {
        console.error('Failed to load traffic detail:', error);
    }
}

// Render traffic detail modal
function renderTrafficDetailModal(detail) {
    currentTrafficDetail = detail;
    const time = new Date(detail.timestamp).toLocaleString();
    const duration = detail.duration < 1000 ? `${detail.duration}ms` : `${(detail.duration / 1000).toFixed(2)}s`;
    const statusClass = detail.statusCode >= 400 || detail.error ? 'error' : 'success';

    const tabData = [
        { key: 'originalRequest', label: t('traffic.originalRequest'), icon: 'arrowUp' },
        { key: 'transformedRequest', label: t('traffic.transformedRequest'), icon: 'refresh' },
        { key: 'originalResponse', label: t('traffic.originalResponse'), icon: 'download' },
        { key: 'transformedResponse', label: t('traffic.transformedResponse'), icon: 'refresh' },
    ];

    const tabsHtml = tabData.map((tab, i) =>
        `<button class="traffic-tab-btn ${i === 0 ? 'active' : ''}" data-tab="${tab.key}" onclick="window.switchTrafficTab('${tab.key}')">
            <span class="icon icon-sm">${getIcon(tab.icon)}</span> ${tab.label}
        </button>`
    ).join('');

    const tabContentsHtml = tabData.map((tab, i) => {
        const raw = detail[tab.key];
        const sourceHtml = formatJSONSource(raw);
        return `
            <div class="traffic-tab-content ${i === 0 ? 'active' : ''}" id="tab-${tab.key}">
                <div class="traffic-json-actions">
                    <button class="btn-json-action" onclick="window.copyTrafficJson('${tab.key}')">${getIcon('copy')} ${t('traffic.copyJson')}</button>
                </div>
                <div class="traffic-json-wrap">${sourceHtml}</div>
            </div>
        `;
    }).join('');

    const content = document.getElementById('trafficDetailContent');
    if (!content) return;

    content.innerHTML = `
        <div class="traffic-detail-header">
            <div class="traffic-detail-status ${statusClass}">
                <span class="status-code">${detail.statusCode || 'ERR'}</span>
                <span class="status-method">${escapeHtml(detail.method || '')}</span>
                <span class="status-path">${escapeHtml(detail.path || '')}</span>
            </div>
            <div class="traffic-detail-meta">
                <span>${t('traffic.endpoint')}: ${escapeHtml(detail.endpointName || '')}</span>
                <span>${t('traffic.transformer')}: ${escapeHtml(detail.transformerName || '-')}</span>
                <span>${t('traffic.duration')}: ${escapeHtml(duration)}</span>
                <span>${t('traffic.time')}: ${escapeHtml(time)}</span>
                ${detail.isStreaming ? `<span class="streaming-badge">SSE</span>` : ''}
                ${detail.truncated ? `<span class="truncated-badge">${t('traffic.truncated')}</span>` : ''}
            </div>
            ${detail.error ? `<div class="traffic-detail-error">${t('traffic.error')}: ${escapeHtml(detail.error)}</div>` : ''}
            ${detail.inputTokens || detail.outputTokens ? `
                <div class="traffic-detail-tokens">
                    ${t('traffic.tokens')}: ${formatTokens(detail.inputTokens)} ${t('traffic.in')} / ${formatTokens(detail.outputTokens)} ${t('traffic.out')}
                </div>
            ` : ''}
        </div>

        <div class="traffic-detail-tabs">${tabsHtml}</div>

        <div class="traffic-detail-body">${tabContentsHtml}</div>
    `;

}

// Switch traffic detail tab
export function switchTrafficTab(tabName) {
    // Update tab buttons
    document.querySelectorAll('.traffic-tab-btn').forEach(btn => {
        btn.classList.toggle('active', btn.dataset.tab === tabName);
    });
    
    // Update tab content
    document.querySelectorAll('.traffic-tab-content').forEach(content => {
        content.classList.toggle('active', content.id === `tab-${tabName}`);
    });
}

// Format JSON for display with collapsible tree
function formatJSON(str) {
    if (!str) return '<span class="json-null">(empty)</span>';

    str = str.trim();
    try {
        const obj = JSON.parse(str);
        return `<div class="json-tree">${renderJSONTree(obj, 0)}</div>`;
    } catch (e) {
        console.warn('JSON parse failed:', e.message);
        return `<pre class="json-raw">${escapeHtml(str)}</pre>`;
    }
}

// Format JSON as source (pretty-printed)
function formatJSONSource(str) {
    if (!str) return '<pre class="json-raw">(empty)</pre>';
    str = str.trim();
    try {
        const obj = JSON.parse(str);
        const pretty = JSON.stringify(obj, null, 2);
        return `<pre class="json-source">${escapeHtml(pretty)}</pre>`;
    } catch (e) {
        return `<pre class="json-raw">${escapeHtml(str)}</pre>`;
    }
}

// Copy JSON content for current tab
export function copyTrafficJson(tabKey) {
    if (!currentTrafficDetail) return;
    const raw = currentTrafficDetail[tabKey];
    if (!raw) return;
    navigator.clipboard.writeText(raw.trim()).then(() => {
        showNotification(t('traffic.copied'), 'success');
    }).catch(() => {});
}

// Escape HTML
function escapeHtml(str) {
    if (str === null || str === undefined) return '';
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}

// Close traffic detail modal
export function closeTrafficDetailModal() {
    const modal = document.getElementById('trafficDetailModal');
    if (modal) modal.style.display = 'none';
}

// Clear traffic logs
export async function clearTrafficLogs() {
    try {
        await window.go.main.App.ClearTrafficLogs();
        await loadTrafficLogs(currentFilter);
    } catch (error) {
        console.error('Failed to clear traffic logs:', error);
    }
}

// Filter by status
export function filterTrafficByStatus(hasError) {
    if (hasError === null) {
        delete currentFilter.hasError;
    } else {
        currentFilter.hasError = hasError;
    }
    loadTrafficLogs(currentFilter);
}

// Filter by endpoint
export function filterTrafficByEndpoint(endpointName) {
    if (!endpointName) {
        delete currentFilter.endpointName;
    } else {
        currentFilter.endpointName = endpointName;
    }
    loadTrafficLogs(currentFilter);
}

// Start auto-refresh
function startAutoRefresh() {
    if (refreshInterval) return;
    refreshInterval = setInterval(() => {
        loadTrafficLogs(currentFilter);
    }, 2000);
}

// Stop auto-refresh
function stopAutoRefresh() {
    if (refreshInterval) {
        clearInterval(refreshInterval);
        refreshInterval = null;
    }
}

// Initialize traffic module
export async function initTraffic() {
    try {
        // Load initial state
        const isRecording = await window.go.main.App.IsTrafficRecording();
        updateRecordingUI(isRecording);
        
        if (isRecording) {
            startAutoRefresh();
        }
        
        // Load initial logs
        await loadTrafficLogs();
    } catch (error) {
        console.error('Failed to initialize traffic module:', error);
        updateRecordingUI(false);
    }
}
