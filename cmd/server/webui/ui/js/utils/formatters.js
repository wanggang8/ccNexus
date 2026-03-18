// Utility functions for formatting data

export function formatNumber(num) {
    if (num >= 1000000) {
        return (num / 1000000).toFixed(1) + 'M';
    }
    if (num >= 1000) {
        return (num / 1000).toFixed(1) + 'K';
    }
    return num.toString();
}

export function formatTokens(tokens) {
    return formatNumber(tokens);
}

export function formatPercentage(value) {
    const sign = value >= 0 ? '+' : '';
    return `${sign}${value.toFixed(1)}%`;
}

export function formatDate(dateString) {
    const date = new Date(dateString);
    return date.toLocaleDateString();
}

export function formatDateTime(dateString) {
    const date = new Date(dateString);
    return date.toLocaleString();
}

export function formatLatency(ms) {
    if (ms < 1000) {
        return `${ms}ms`;
    }
    return `${(ms / 1000).toFixed(2)}s`;
}

export function getTransformerLabel(transformer) {
    const labels = {
        'claude': 'Claude',
        'openai': 'OpenAI',
        'openai2': 'OpenAI Responses',
        'gemini': 'Gemini',
        'cli': 'Claude CLI'
    };
    return labels[transformer] || transformer;
}

export function getStatusBadge(enabled) {
    if (enabled) {
        return '<span class="badge badge-success">Enabled</span>';
    }
    return '<span class="badge badge-danger">Disabled</span>';
}

export function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

export function formatJSON(str) {
    if (!str) return escapeHtml('(empty)');
    try {
        const obj = JSON.parse(str);
        return escapeHtml(JSON.stringify(obj, null, 2));
    } catch {
        return escapeHtml(str);
    }
}

export function copyText(text) {
    if (!text) return Promise.reject(new Error('Nothing to copy'));

    if (typeof navigator !== 'undefined' &&
        navigator.clipboard &&
        typeof navigator.clipboard.writeText === 'function') {
        return navigator.clipboard.writeText(text);
    }

    return new Promise((resolve, reject) => {
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
            ok ? resolve() : reject(new Error('execCommand failed'));
        } catch (e) {
            reject(e);
        }
    });
}
