// 端点筛选功能模块
import { getEndpointTestStatus } from './endpoints.js';
import { t } from '../i18n/index.js';
import { getIcon } from '../icons.js';

// 筛选状态（多选数组）
let filterState = {
    types: [],            // ['claude', 'gemini', 'openai', 'openai2']
    availabilities: [],   // ['available', 'unknown', 'unavailable']
    enabledStates: []     // ['enabled', 'disabled']
};

// 获取当前筛选状态
export function getFilterState() {
    return { ...filterState };
}

// 检查是否有激活的筛选
export function isFilterActive() {
    return filterState.types.length > 0 ||
           filterState.availabilities.length > 0 ||
           filterState.enabledStates.length > 0;
}

// 清除所有筛选
export function clearAllFilters() {
    filterState = {
        types: [],
        availabilities: [],
        enabledStates: []
    };

    syncTempStateFromFilterState();

    // 清空所有复选框
    document.querySelectorAll('.filter-dropdown-panel input[type="checkbox"], #unifiedFilterPanel input[type="checkbox"]').forEach(cb => {
        cb.checked = false;
    });

    saveFilterState();
    updateAllBadges();
    applyFilters();
}

// 持久化筛选状态
const FILTER_STATE_KEY = 'ccnexus_filter_state_v3';

function renderUnifiedFilterOptions() {
    const groups = [
        {
            containerId: 'filterTypeOptions',
            filterType: 'types',
            options: [
                { value: 'claude', label: 'Claude' },
                { value: 'openai', label: 'OpenAI' },
                { value: 'openai2', label: 'OpenAI Responses' },
                { value: 'gemini', label: 'Gemini' },
                { value: 'cli', label: 'CLI' }
            ]
        },
        {
            containerId: 'filterAvailabilityOptions',
            filterType: 'availabilities',
            options: [
                { value: 'available', label: t('endpoints.filterAvailable') },
                { value: 'unknown', label: t('endpoints.filterUnknown') },
                { value: 'unavailable', label: t('endpoints.filterUnavailable') }
            ]
        },
        {
            containerId: 'filterEnabledOptions',
            filterType: 'enabledStates',
            options: [
                { value: 'enabled', label: t('endpoints.filterEnabled') },
                { value: 'disabled', label: t('endpoints.filterDisabled') }
            ]
        }
    ];

    groups.forEach(({ containerId, filterType, options }) => {
        const container = document.getElementById(containerId);
        if (!container) return;

        container.dataset.filterType = filterType;
        container.innerHTML = options.map(({ value, label }) => `
            <label>
                <input type="checkbox" value="${value}">
                <span>${label}</span>
            </label>
        `).join('');
    });
}

function syncTempStateFromFilterState() {
    tempState = {
        types: [...filterState.types],
        availabilities: [...filterState.availabilities],
        enabledStates: [...filterState.enabledStates]
    };
}

function syncUnifiedCheckboxesFromState() {
    const filterPanel = document.getElementById('unifiedFilterPanel');
    if (!filterPanel) return;

    const stateMap = {
        types: filterState.types,
        availabilities: filterState.availabilities,
        enabledStates: filterState.enabledStates
    };

    Object.entries(stateMap).forEach(([filterType, values]) => {
        filterPanel.querySelectorAll(`[data-filter-type="${filterType}"] input[type="checkbox"]`).forEach(cb => {
            cb.checked = values.includes(cb.value);
        });
    });
}

function saveFilterState() {
    try {
        localStorage.setItem(FILTER_STATE_KEY, JSON.stringify(filterState));
    } catch (error) {
        console.error('Failed to save filter state:', error);
    }
}

function loadFilterState() {
    try {
        const saved = localStorage.getItem(FILTER_STATE_KEY);
        if (saved) {
            filterState = { ...filterState, ...JSON.parse(saved) };
        }
    } catch (error) {
        console.error('Failed to load filter state:', error);
    }
}

// 初始化筛选下拉框
export function initFilterDropdowns() {
    loadFilterState();
    syncTempStateFromFilterState();
    renderUnifiedFilterOptions();

    // 初始化统一筛选面板
    initUnifiedFilterPanel();

    // 为每个下拉按钮绑定事件（保留旧代码兼容性）
    document.querySelectorAll('.filter-dropdown').forEach(dropdown => {
        const btn = dropdown.querySelector('.filter-dropdown-btn');
        const panel = dropdown.querySelector('.filter-dropdown-panel');
        const filterKey = dropdown.dataset.filter; // 'types' | 'availabilities' | 'enabledStates'

        // 点击按钮切换面板
        btn.addEventListener('click', (e) => {
            e.stopPropagation();
            togglePanel(dropdown);
        });

        // 复选框变化时更新临时状态（不立即应用）
        const checkboxes = panel.querySelectorAll('input[type="checkbox"]');
        checkboxes.forEach(cb => {
            // 初始化勾选状态
            cb.checked = filterState[filterKey].includes(cb.value);

            cb.addEventListener('change', () => {
                updateTempState(filterKey, checkboxes);
            });
        });

        // "清空"按钮
        panel.querySelector('.btn-clear-dimension')?.addEventListener('click', () => {
            checkboxes.forEach(cb => cb.checked = false);
            updateTempState(filterKey, checkboxes);
        });

        // "确定"按钮
        panel.querySelector('.btn-apply')?.addEventListener('click', () => {
            applyTempState(filterKey, checkboxes);
            closePanel(dropdown);
        });
    });

    // 点击外部关闭所有面板
    document.addEventListener('click', () => {
        closeAllPanels();
        closeUnifiedFilterPanel();
    });

    // 阻止面板内点击冒泡
    document.querySelectorAll('.filter-dropdown-panel').forEach(panel => {
        panel.addEventListener('click', (e) => {
            e.stopPropagation();
        });
    });

    // 初始化徽章显示
    updateAllBadges();
}

// 初始化统一筛选面板
function initUnifiedFilterPanel() {
    const filterBtn = document.getElementById('unifiedFilterBtn');
    const filterPanel = document.getElementById('unifiedFilterPanel');

    if (!filterBtn || !filterPanel) return;

    // 点击按钮切换面板
    filterBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        toggleUnifiedFilterPanel();
    });

    // 阻止面板内点击冒泡
    filterPanel.addEventListener('click', (e) => {
        e.stopPropagation();
    });

    // 初始化所有复选框状态
    initUnifiedFilterCheckboxes();

    const closeBtn = filterPanel.querySelector('.btn-close');
    closeBtn?.addEventListener('click', () => {
        closeUnifiedFilterPanel();
    });

    // "清空全部"按钮
    const clearAllBtn = filterPanel.querySelector('.btn-clear-all');
    clearAllBtn?.addEventListener('click', () => {
        filterPanel.querySelectorAll('input[type="checkbox"]').forEach(cb => {
            cb.checked = false;
        });
        updateUnifiedTempState();
    });

    // "确定"按钮
    const applyBtn = filterPanel.querySelector('.btn-apply-unified');
    applyBtn?.addEventListener('click', () => {
        applyUnifiedFilter();
    });
}

// 初始化统一筛选面板的复选框
function initUnifiedFilterCheckboxes() {
    const filterPanel = document.getElementById('unifiedFilterPanel');
    if (!filterPanel) return;

    syncUnifiedCheckboxesFromState();

    filterPanel.querySelectorAll('input[type="checkbox"]').forEach(cb => {
        cb.addEventListener('change', updateUnifiedTempState);
    });

    updateUnifiedTempState();
}

// 更新统一筛选的临时状态
function updateUnifiedTempState() {
    const filterPanel = document.getElementById('unifiedFilterPanel');
    if (!filterPanel) return;

    tempState.types = Array.from(filterPanel.querySelectorAll('[data-filter-type="types"] input:checked'))
        .map(cb => cb.value);
    tempState.availabilities = Array.from(filterPanel.querySelectorAll('[data-filter-type="availabilities"] input:checked'))
        .map(cb => cb.value);
    tempState.enabledStates = Array.from(filterPanel.querySelectorAll('[data-filter-type="enabledStates"] input:checked'))
        .map(cb => cb.value);
}

// 应用统一筛选
export function applyUnifiedFilter() {
    filterState.types = tempState.types || [];
    filterState.availabilities = tempState.availabilities || [];
    filterState.enabledStates = tempState.enabledStates || [];

    saveFilterState();
    updateAllBadges();
    applyFilters();
    closeUnifiedFilterPanel();
}

// 切换统一筛选面板
export function toggleUnifiedFilterPanel() {
    const filterPanel = document.getElementById('unifiedFilterPanel');
    const filterBtn = document.getElementById('unifiedFilterBtn');

    if (!filterPanel || !filterBtn) return;

    const isOpen = !filterPanel.classList.contains('hidden');

    if (isOpen) {
        closeUnifiedFilterPanel();
    } else {
        closeAllPanels(); // 关闭其他面板
        syncTempStateFromFilterState();
        syncUnifiedCheckboxesFromState();
        
        // 动态计算位置 - 面板右对齐按钮
        const btnRect = filterBtn.getBoundingClientRect();
        const panelWidth = 420; // 面板宽度
        filterPanel.style.top = `${btnRect.bottom + 8}px`;
        filterPanel.style.left = `${btnRect.right - panelWidth}px`;
        
        filterPanel.classList.remove('hidden');
        filterBtn.classList.add('active');
    }
}

// 关闭统一筛选面板
export function closeUnifiedFilterPanel() {
    const filterPanel = document.getElementById('unifiedFilterPanel');
    const filterBtn = document.getElementById('unifiedFilterBtn');

    if (filterPanel) {
        filterPanel.classList.add('hidden');
    }
    if (filterBtn && !isFilterActive()) {
        filterBtn.classList.remove('active');
    }
}

// 更新统一筛选按钮徽章
function updateUnifiedFilterBadge() {
    const totalCount = filterState.types.length +
                      filterState.availabilities.length +
                      filterState.enabledStates.length;

    const badge = document.getElementById('unifiedFilterBadge');
    const btn = document.getElementById('unifiedFilterBtn');

    if (badge) {
        if (totalCount > 0) {
            badge.textContent = totalCount;
            badge.classList.remove('hidden');
            btn?.classList.add('active');
        } else {
            badge.classList.add('hidden');
            btn?.classList.remove('active');
        }
    }
}

// 切换面板显示
function togglePanel(dropdown) {
    const panel = dropdown.querySelector('.filter-dropdown-panel');
    const arrow = dropdown.querySelector('.filter-arrow');
    const isOpen = !panel.classList.contains('hidden');

    // 关闭所有其他面板
    closeAllPanels();

    if (!isOpen) {
        panel.classList.remove('hidden');
        arrow.innerHTML = getIcon('chevronUp');
    }
}

// 关闭所有面板
function closeAllPanels() {
    document.querySelectorAll('.filter-dropdown-panel').forEach(panel => {
        panel.classList.add('hidden');
    });
    document.querySelectorAll('.filter-arrow').forEach(arrow => {
        arrow.innerHTML = getIcon('chevronDown');
    });
}

// 关闭单个面板
function closePanel(dropdown) {
    dropdown.querySelector('.filter-dropdown-panel').classList.add('hidden');
    dropdown.querySelector('.filter-arrow').innerHTML = getIcon('chevronDown');
}

// 临时状态（用于"确定"前的预览）
let tempState = {};

// 更新临时状态（仅内存，不持久化，不应用筛选）
function updateTempState(filterKey, checkboxes) {
    tempState[filterKey] = Array.from(checkboxes)
        .filter(cb => cb.checked)
        .map(cb => cb.value);
}

// 应用临时状态到实际筛选
function applyTempState(filterKey, checkboxes) {
    filterState[filterKey] = Array.from(checkboxes)
        .filter(cb => cb.checked)
        .map(cb => cb.value);

    saveFilterState();
    updateBadge(filterKey);
    applyFilters();
}

// 更新徽章显示
function updateBadge(filterKey) {
    const count = filterState[filterKey].length;
    const badge = document.getElementById(`filterBadge${capitalizeFirst(filterKey)}`);
    const btn = badge?.closest('.filter-dropdown-btn');

    if (badge) {
        if (count > 0) {
            badge.textContent = count;
            badge.classList.remove('hidden');
            btn?.classList.add('active');
        } else {
            badge.classList.add('hidden');
            btn?.classList.remove('active');
        }
    }
}

function updateAllBadges() {
    updateBadge('types');
    updateBadge('availabilities');
    updateBadge('enabledStates');
    updateUnifiedFilterBadge();
}

function capitalizeFirst(str) {
    return str.charAt(0).toUpperCase() + str.slice(1);
}

// 应用筛选
function applyFilters() {
    // 显示/隐藏筛选激活提示条
    const banner = document.getElementById('filterActiveBanner');
    if (isFilterActive()) {
        banner?.classList.remove('hidden');
    } else {
        banner?.classList.add('hidden');
    }

    // 更新清除筛选按钮显示
    const clearBtn = document.getElementById('filterClearBtn');
    if (clearBtn) {
        if (isFilterActive()) {
            clearBtn.classList.remove('hidden');
        } else {
            clearBtn.classList.add('hidden');
        }
    }

    // 触发端点列表重新渲染
    if (window.loadConfig) {
        window.loadConfig();
    }
}

// 筛选端点（多选逻辑：维度内 OR，维度间 AND）
export function filterEndpoints(endpoints) {
    const { types, availabilities, enabledStates } = filterState;

    return endpoints.filter(ep => {
        // 1. 类型筛选（维度内 OR）
        if (types.length > 0) {
            const epType = ep.transformer || 'claude';
            if (!types.includes(epType)) return false;
        }

        // 2. 可用性筛选（维度内 OR）
        if (availabilities.length > 0) {
            const testStatus = getEndpointTestStatus(ep.name);
            let matchesAvailability = false;

            for (const av of availabilities) {
                if (av === 'available' && testStatus === true) {
                    matchesAvailability = true;
                    break;
                }
                if (av === 'unknown' && (testStatus === undefined || testStatus === 'unknown')) {
                    matchesAvailability = true;
                    break;
                }
                if (av === 'unavailable' && testStatus === false) {
                    matchesAvailability = true;
                    break;
                }
            }

            if (!matchesAvailability) return false;
        }

        // 3. 启用状态筛选（维度内 OR）
        if (enabledStates.length > 0) {
            const isEnabled = ep.enabled !== undefined ? ep.enabled : true;
            let matchesEnabled = false;

            for (const state of enabledStates) {
                if (state === 'enabled' && isEnabled) {
                    matchesEnabled = true;
                    break;
                }
                if (state === 'disabled' && !isEnabled) {
                    matchesEnabled = true;
                    break;
                }
            }

            if (!matchesEnabled) return false;
        }

        return true;
    });
}

// 更新筛选统计
export function updateFilterStats(total, filtered) {
    const statsEl = document.getElementById('filterStats');
    if (statsEl) {
        if (total === filtered) {
            statsEl.textContent = t('endpoints.filterStatsTotal').replace('{total}', total);
        } else {
            statsEl.textContent = t('endpoints.filterStatsFiltered')
                .replace('{filtered}', filtered)
                .replace('{total}', total);
        }
    }
}

// 全局方法由 main.js 统一挂载
