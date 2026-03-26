import { t } from '../i18n/index.js';
import { escapeHtml, formatTokens } from '../utils/format.js';
import { showConfirm, showNotification } from './modal.js';

let trafficLoadVersion = 0;
let trafficClearInProgress = false;

function formatDateTime(timestamp) {
    if (!timestamp) return '-';
    return new Date(timestamp).toLocaleString();
}

function formatLatency(durationMs) {
    if (!durationMs) return '0ms';
    if (durationMs < 1000) return `${durationMs}ms`;
    return `${(durationMs / 1000).toFixed(2)}s`;
}

function prettyPrint(value) {
    if (!value) return '(empty)';
    try {
        return JSON.stringify(JSON.parse(value), null, 2);
    } catch (_) {
        return value;
    }
}

function getStatusLabel(log) {
    if (log.error) {
        return { className: 'error', text: log.statusCode || 'ERR' };
    }
    if ((log.statusCode || 0) >= 400) {
        return { className: 'warn', text: log.statusCode };
    }
    if (log.statusCode) {
        return { className: 'ok', text: log.statusCode };
    }
    return { className: 'info', text: 'N/A' };
}

function readTrafficFilter() {
    const endpointName = document.getElementById('trafficEndpointFilter')?.value?.trim() || '';
    const clientFormat = document.getElementById('trafficFormatFilter')?.value || '';
    const hasError = document.getElementById('trafficErrorFilter')?.value || '';

    const filter = { limit: 10 };
    if (endpointName) filter.endpointName = endpointName;
    if (clientFormat) filter.clientFormat = clientFormat;
    if (hasError !== '') filter.hasError = hasError === 'true';
    return filter;
}

export async function loadTraffic() {
    if (trafficClearInProgress) {
        return false;
    }
    const loadVersion = ++trafficLoadVersion;
    try {
        if (!window.go?.main?.App) return false;

        const resultStr = await window.go.main.App.GetTrafficLogs(JSON.stringify(readTrafficFilter()));
        const result = JSON.parse(resultStr);
        if (loadVersion !== trafficLoadVersion) return false;
        renderTraffic(result.logs || []);

        const summary = document.getElementById('trafficSummary');
        if (summary) {
            summary.textContent = t('traffic.summary')
                .replace('{count}', result.count || 0)
                .replace('{total}', result.total || 0);
        }

        const recordingToggle = document.getElementById('trafficRecordingToggle');
        if (recordingToggle) {
            recordingToggle.checked = Boolean(result.recording);
        }
        return true;
    } catch (error) {
        if (loadVersion !== trafficLoadVersion) return false;
        console.error('Failed to load traffic logs:', error);
        return false;
    }
}

function renderTraffic(logs) {
    const tbody = document.getElementById('trafficTableBody');
    if (!tbody) return;

    if (!logs.length) {
        tbody.innerHTML = `
            <tr>
                <td colspan="8" class="traffic-empty">${t('traffic.empty')}</td>
            </tr>
        `;
        return;
    }

    tbody.innerHTML = logs.map((log) => {
        const status = getStatusLabel(log);
        return `
            <tr>
                <td>${escapeHtml(formatDateTime(log.timestamp))}</td>
                <td>${escapeHtml(log.endpointName || '-')}</td>
                <td>${escapeHtml(log.clientFormat || '-')}</td>
                <td><code>${escapeHtml(log.eventType || '-')}</code></td>
                <td><span class="traffic-status traffic-status-${status.className}">${escapeHtml(String(status.text))}</span></td>
                <td>${escapeHtml(formatLatency(log.duration || 0))}</td>
                <td>${escapeHtml(`${formatTokens(log.inputTokens || 0)} / ${formatTokens(log.outputTokens || 0)}`)}</td>
                <td>
                    <button class="btn btn-secondary btn-sm" onclick="window.showTrafficDetail('${escapeHtml(log.id)}')">
                        ${t('traffic.inspect')}
                    </button>
                </td>
            </tr>
        `;
    }).join('');
}

export async function showTrafficDetail(id) {
    try {
        const detailStr = await window.go.main.App.GetTrafficLogDetail(id);
        const detail = JSON.parse(detailStr);
        if (detail.error) {
            showNotification(t('traffic.detailLoadFailed'), 'error');
            return;
        }

        document.getElementById('trafficDetailMeta').innerHTML = `
            <div class="traffic-detail-pill"><span>${t('traffic.requestId')}</span><strong>${escapeHtml(detail.requestId || '-')}</strong></div>
            <div class="traffic-detail-pill"><span>${t('traffic.endpoint')}</span><strong>${escapeHtml(detail.endpointName || '-')}</strong></div>
            <div class="traffic-detail-pill"><span>${t('traffic.path')}</span><strong>${escapeHtml(detail.path || '-')}</strong></div>
            <div class="traffic-detail-pill"><span>${t('traffic.streaming')}</span><strong>${detail.isStreaming ? t('traffic.streamingYes') : t('traffic.streamingNo')}</strong></div>
        `;

        document.getElementById('trafficOriginalRequest').textContent = prettyPrint(detail.originalRequest);
        document.getElementById('trafficTransformedRequest').textContent = prettyPrint(detail.transformedRequest);
        document.getElementById('trafficOriginalResponse').textContent = prettyPrint(detail.originalResponse);
        document.getElementById('trafficTransformedResponse').textContent = prettyPrint(detail.transformedResponse);

        document.getElementById('trafficDetailModal').classList.add('active');
    } catch (error) {
        console.error('Failed to load traffic detail:', error);
        showNotification(t('traffic.detailLoadFailed'), 'error');
    }
}

export function closeTrafficDetailModal() {
    document.getElementById('trafficDetailModal')?.classList.remove('active');
}

export async function toggleTrafficRecording() {
    const enabled = document.getElementById('trafficRecordingToggle')?.checked || false;
    try {
        await window.go.main.App.SetTrafficRecording(enabled);
        showNotification(enabled ? t('traffic.recordingEnabled') : t('traffic.recordingDisabled'), 'success');
        await loadTraffic();
    } catch (error) {
        console.error('Failed to toggle traffic recording:', error);
        showNotification(t('traffic.recordingToggleFailed'), 'error');
        await loadTraffic();
    }
}

export async function clearTrafficLogs() {
    const confirmed = await showConfirm(t('traffic.clearConfirm'));
    if (!confirmed) {
        return;
    }

    let cleared = false;
    try {
        trafficClearInProgress = true;
        trafficLoadVersion++;
        await window.go.main.App.ClearTrafficLogs();
        renderTraffic([]);
        const summary = document.getElementById('trafficSummary');
        if (summary) {
            summary.textContent = t('traffic.summary')
                .replace('{count}', 0)
                .replace('{total}', 0);
        }
        cleared = true;
        showNotification(t('traffic.cleared'), 'success');
    } catch (error) {
        console.error('Failed to clear traffic logs:', error);
        showNotification(t('traffic.clearFailed'), 'error');
    } finally {
        trafficClearInProgress = false;
        if (!cleared) {
            await loadTraffic();
        }
    }
}

export async function applyTrafficFilters() {
    await loadTraffic();
}

export async function refreshTrafficLogs() {
    try {
        const ok = await loadTraffic();
        if (!ok) {
            showNotification(t('traffic.refreshFailed'), 'error');
            return;
        }
        showNotification(t('traffic.refreshed'), 'success');
    } catch (error) {
        console.error('Failed to refresh traffic logs:', error);
        showNotification(t('traffic.refreshFailed'), 'error');
    }
}
