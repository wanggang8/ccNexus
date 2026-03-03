import { t } from '../i18n/index.js';
import { getIcon } from '../icons.js';

let currentWorkspaceTab = 'endpoints';

// Switch workspace tab
export function switchWorkspaceTab(tabName) {
    currentWorkspaceTab = tabName;
    
    // Update tab buttons
    document.querySelectorAll('.workspace-tab').forEach(btn => {
        btn.classList.toggle('active', btn.dataset.tab === tabName);
    });
    
    // Update panels
    document.querySelectorAll('.workspace-panel').forEach(panel => {
        panel.classList.toggle('active', panel.id === `panel-${tabName}`);
    });
    
    // Update action buttons
    document.querySelectorAll('.tab-actions').forEach(actions => {
        actions.classList.remove('active');
    });
    const actionsEl = document.getElementById(`${tabName}Actions`);
    if (actionsEl) actionsEl.classList.add('active');
}

export function initUI() {
    const platform = navigator.platform.toLowerCase();
    const isShowBtn = platform.includes('win') || platform.includes('mac');

    const app = document.getElementById('app');
    app.innerHTML = `
        <!-- 页面右上角斜拉横幅 -->
        <div class="ribbon-banner hidden" onclick="window.showSponsorModal()" title="${t('sponsor.ribbonTip')}">${t('sponsor.ribbon')}</div>

        <div class="header">
            <div style="display: flex; justify-content: space-between; align-items: center; width: 100%;">
                <div>
                    <h1><span class="icon icon-lg">${getIcon('rocket')}</span> ${t('app.title')}<span id="broadcast-banner" class="broadcast-banner hidden"></span></h1>
                    <p>${t('header.title')}<span id="festivalToggle" class="festival-toggle hidden" onclick="window.toggleFestivalEffect(); event.stopPropagation();" title="${t('festival.toggle') || '切换氛围效果'}"><span class="festival-toggle-name" id="festivalToggleName"></span><span class="festival-toggle-switch" id="festivalToggleSwitch"></span></span></p>
                </div>
                <div style="display: flex; gap: 15px; align-items: center;">
                    <div class="port-display" onclick="window.showEditPortModal()" title="${t('header.port')}">
                        <span style="color: #666; font-size: 15px; position: relative; top: -0.3px;">${t('header.port')}: </span>
                        <span class="port-number" id="proxyPort">3000</span>
                    </div>
                    <div style="display: flex; gap: 10px;">
                        <button class="header-link" onclick="window.openGitHub()" title="${t('header.githubRepo')}">
                            <span class="icon">${getIcon('github')}</span>
                        </button>
                        <button class="header-link about-btn" id="aboutBtn" onclick="window.showWelcomeModal()" title="${t('header.about')}">
                            <span class="icon">${getIcon('book')}</span>
                            <span class="update-badge" id="updateBadge"></span>
                        </button>
                        <button class="header-link" onclick="window.showSettingsModal()" title="${t('settings.title')}">
                            <span class="icon">${getIcon('settings')}</span>
                        </button>
                    </div>
                </div>
            </div>
        </div>

        <div class="container">
            <!-- Statistics -->
            <div class="card">
                <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 15px;">
                    <h2 style="margin: 0;"><span class="icon icon-lg">${getIcon('chart')}</span> ${t('statistics.title')}</h2>
                    <div class="stats-tabs">
                        <button class="stats-tab-btn active" data-period="daily" onclick="window.switchStatsPeriod('daily')">
                            <span class="icon">${getIcon('calendar')}</span> ${t('statistics.daily')}
                        </button>
                        <button class="stats-tab-btn" data-period="yesterday" onclick="window.switchStatsPeriod('yesterday')">
                            <span class="icon">${getIcon('calendar')}</span> ${t('statistics.yesterday')}
                        </button>
                        <button class="stats-tab-btn" data-period="weekly" onclick="window.switchStatsPeriod('weekly')">
                            <span class="icon">${getIcon('chart')}</span> ${t('statistics.weekly')}
                        </button>
                        <button class="stats-tab-btn" data-period="monthly" onclick="window.switchStatsPeriod('monthly')">
                            <span class="icon">${getIcon('trendUp')}</span> ${t('statistics.monthly')}
                        </button>
                        <button class="stats-tab-btn" data-period="history" onclick="window.switchStatsPeriod('history')">
                            <span class="icon">${getIcon('activity')}</span> ${t('statistics.history')}
                        </button>
                    </div>
                </div>

                <!-- Current Stats View -->
                <div id="currentStatsView">
                    <div class="stats-grid">
                    <div class="stat-box">
                        <div class="stat-header">
                            <div class="stat-label">${t('statistics.endpoints')}</div>
                        </div>
                        <div class="stat-value">
                            <span id="activeEndpointsDisplay" class="stat-primary">0</span>
                            <span class="stat-secondary"> / </span>
                            <span id="totalEndpointsDisplay" class="stat-secondary">0</span>
                        </div>
                        <div class="stat-detail">${t('statistics.activeTotal')}</div>
                    </div>
                    <div class="stat-box">
                        <div class="stat-header">
                            <div class="stat-label">${t('statistics.totalRequests')}</div>
                            <span class="trend" id="requestsTrend"><span class="icon">${getIcon('arrowRight')}</span> 0%</span>
                        </div>
                        <div class="stat-value">
                            <span id="periodTotalRequests">0</span>
                        </div>
                        <div class="stat-detail">
                            <span id="periodSuccess">0</span>
                            <span class="stat-text"> ${t('statistics.success')}</span>
                            <span class="stat-divider">/</span>
                            <span id="periodFailed">0</span>
                            <span class="stat-text"> ${t('statistics.failed')}</span>
                        </div>
                    </div>
                    <div class="stat-box">
                        <div class="stat-header">
                            <div class="stat-label">${t('statistics.totalTokens')}</div>
                            <span class="trend" id="tokensTrend"><span class="icon">${getIcon('arrowRight')}</span> 0%</span>
                        </div>
                        <div class="stat-value">
                            <span id="periodTotalTokens">0</span>
                        </div>
                        <div class="stat-detail">
                            <span id="periodInputTokens">0</span>
                            <span class="stat-text"> ${t('statistics.in')}</span>
                            <span class="stat-divider">/</span>
                            <span id="periodOutputTokens">0</span>
                            <span class="stat-text"> ${t('statistics.out')}</span>
                        </div>
                    </div>
                </div>

                <!-- Hidden cumulative stats for endpoint cards -->
                <div style="display: none;">
                    <span id="activeEndpoints">0</span>
                    <span id="totalEndpoints">0</span>
                    <span id="totalRequests">0</span>
                    <span id="successRequests">0</span>
                    <span id="failedRequests">0</span>
                    <span id="totalTokens">0</span>
                    <span id="totalInputTokens">0</span>
                    <span id="totalOutputTokens">0</span>
                </div>
                </div>
            </div>

            <!-- History Modal (弹窗) -->
            <div id="historyModal" class="modal" style="display: none;">
                <div class="modal-content">
                    <div class="modal-header">
                        <h2><span class="icon icon-lg">${getIcon('activity')}</span> ${t('history.title')}</h2>
                        <button class="modal-close" onclick="window.closeHistoryModal()">&times;</button>
                    </div>
                    <div class="modal-body">
                        <div class="history-selector">
                            <label>${t('history.selectMonth')}:</label>
                            <select id="historyMonthSelect"></select>
                        </div>

                        <div class="history-stats-wrapper">
                            <div class="stats-grid">
                            <div class="stat-box">
                                <div class="stat-header">
                                    <div class="stat-label">${t('statistics.totalRequests')}</div>
                                    <span class="trend" id="historyRequestsTrend"><span class="icon">${getIcon('arrowRight')}</span> 0%</span>
                                </div>
                                <div class="stat-value">
                                    <span id="historyTotalRequests">0</span>
                                </div>
                                <div class="stat-detail">
                                    <span id="historySuccess">0</span>
                                    <span class="stat-text"> ${t('statistics.success')}</span>
                                    <span class="stat-divider">/</span>
                                    <span id="historyFailed">0</span>
                                    <span class="stat-text"> ${t('statistics.failed')}</span>
                                </div>
                            </div>
                            <div class="stat-box">
                                <div class="stat-header">
                                    <div class="stat-label">${t('statistics.totalTokens')}</div>
                                    <span class="trend" id="historyTokensTrend"><span class="icon">${getIcon('arrowRight')}</span> 0%</span>
                                </div>
                                <div class="stat-value">
                                    <span id="historyTotalTokens">0</span>
                                </div>
                                <div class="stat-detail">
                                    <span id="historyInputTokens">0</span>
                                    <span class="stat-text"> ${t('statistics.in')}</span>
                                    <span class="stat-divider">/</span>
                                    <span id="historyOutputTokens">0</span>
                                    <span class="stat-text"> ${t('statistics.out')}</span>
                                </div>
                            </div>
                        </div>
                        </div>

                        <div class="history-details">
                            <div class="history-details-header">
                                <h3>${t('history.dailyDetails')}</h3>
                                <button id="historyDeleteBtn" class="history-delete-btn" onclick="window.deleteHistoryArchive()" title="${t('history.deleteTitle')}">
                                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                        <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"></path>
                                    </svg>
                                    ${t('history.delete')}
                                </button>
                            </div>
                            <div class="table-container">
                                <table id="historyDailyTable">
                                    <thead>
                                        <tr>
                                            <th>${t('history.date')}</th>
                                            <th>${t('history.requests')}</th>
                                            <th>${t('history.errors')}</th>
                                            <th>${t('history.inputTokens')}</th>
                                            <th>${t('history.outputTokens')}</th>
                                            <th>${t('history.totalTokens')}</th>
                                        </tr>
                                    </thead>
                                    <tbody></tbody>
                                </table>
                            </div>
                        </div>

                        <div id="historyError" class="error-message" style="display: none;"></div>
                    </div>
                </div>
            </div>

            <!-- Workspace Tab Card -->
            <div class="card workspace-card">
                <div class="workspace-header">
                    <div class="workspace-tabs">
                        <button class="workspace-tab active" data-tab="endpoints" onclick="window.switchWorkspaceTab('endpoints')">
                            <span class="icon">${getIcon('server')}</span> ${t('endpoints.title')}
                        </button>
                        <button class="workspace-tab" data-tab="traffic" onclick="window.switchWorkspaceTab('traffic')">
                            <span class="icon">${getIcon('activity')}</span> ${t('traffic.title')} <span class="tab-badge" id="trafficTabBadge"></span>
                        </button>
                        <button class="workspace-tab" data-tab="logs" onclick="window.switchWorkspaceTab('logs')">
                            <span class="icon">${getIcon('terminal')}</span> ${t('logs.title')}
                        </button>
                    </div>
                    <div class="workspace-actions">
                        <!-- Endpoints Actions -->
                        <div id="endpointsActions" class="tab-actions active">
                            <div class="view-mode-tabs">
                                <button class="view-mode-btn active" data-view="detail" onclick="window.switchEndpointViewMode('detail')" title="${t('endpoints.viewDetail')}">
                                    <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
                                        <rect x="3" y="3" width="8" height="8" rx="1"/>
                                        <rect x="13" y="3" width="8" height="8" rx="1"/>
                                        <rect x="3" y="13" width="8" height="8" rx="1"/>
                                        <rect x="13" y="13" width="8" height="8" rx="1"/>
                                    </svg>
                                </button>
                                <button class="view-mode-btn" data-view="compact" onclick="window.switchEndpointViewMode('compact')" title="${t('endpoints.viewCompact')}">
                                    <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
                                        <rect x="3" y="4" width="18" height="3" rx="1"/>
                                        <rect x="3" y="10.5" width="18" height="3" rx="1"/>
                                        <rect x="3" y="17" width="18" height="3" rx="1"/>
                                    </svg>
                                </button>
                            </div>
                            ${isShowBtn ? `
                            <button class="btn btn-secondary" onclick="window.showTerminalModal()">
                                <span class="icon">${getIcon('terminal')}</span> ${t('terminal.title')}
                            </button>` : ''}
                            <button class="btn btn-secondary" onclick="window.showDataSyncDialog()">
                                <span class="icon">${getIcon('refresh')}</span> ${t('webdav.dataSync')}
                            </button>
                            <button class="btn btn-primary" onclick="window.showAddEndpointModal()">
                                <span class="icon">${getIcon('plus')}</span> ${t('header.addEndpoint')}
                            </button>
                        </div>
                        <!-- Traffic Actions -->
                        <div id="trafficActions" class="tab-actions">
                            <span class="traffic-count" id="trafficCount">0 / 0</span>
                            <select class="traffic-filter-select" onchange="window.filterTrafficByStatus(this.value === '' ? null : this.value === 'true')">
                                <option value="">${t('traffic.filterAll')}</option>
                                <option value="true">${t('traffic.filterErrors')}</option>
                                <option value="false">${t('traffic.filterSuccess')}</option>
                            </select>
                            <button class="btn btn-secondary btn-sm" onclick="window.clearTrafficLogs()">
                                <span class="icon">${getIcon('trash')}</span> ${t('traffic.clear')}
                            </button>
                            <button id="trafficRecordBtn" class="traffic-record-btn" onclick="window.toggleTrafficRecording()">
                                <span class="record-dot"></span> ${t('traffic.startRecording')}
                            </button>
                        </div>
                        <!-- Logs Actions -->
                        <div id="logsActions" class="tab-actions">
                            <select id="logLevel" class="log-level-select-btn" onchange="window.changeLogLevel()">
                                <option value="0"><span class="icon">${getIcon('search')}</span> ${t('logs.levels.0')}</option>
                                <option value="1" selected><span class="icon">${getIcon('info')}</span> ${t('logs.levels.1')}</option>
                                <option value="2"><span class="icon">${getIcon('alertTriangle')}</span> ${t('logs.levels.2')}</option>
                                <option value="3"><span class="icon">${getIcon('x')}</span> ${t('logs.levels.3')}</option>
                            </select>
                            <button class="btn btn-secondary btn-sm" onclick="window.copyLogs()">
                                <span class="icon">${getIcon('copy')}</span> ${t('logs.copy')}
                            </button>
                            <button class="btn btn-secondary btn-sm" onclick="window.clearLogs()">
                                <span class="icon">${getIcon('trash')}</span> ${t('logs.clear')}
                            </button>
                        </div>
                    </div>
                </div>
                <div class="workspace-content">
                    <!-- Endpoints Panel -->
                    <div class="workspace-panel active" id="panel-endpoints">
                        <div id="endpointList" class="endpoint-list">
                            <div class="loading">${t('endpoints.title')}...</div>
                        </div>
                    </div>
                    <!-- Traffic Panel -->
                    <div class="workspace-panel" id="panel-traffic">
                        <div class="traffic-log-header">
                            <div class="traffic-col-status">${t('traffic.status')}</div>
                            <div class="traffic-col-info">${t('traffic.request')}</div>
                            <div class="traffic-col-tokens">${t('traffic.tokens')}</div>
                            <div class="traffic-col-duration">${t('traffic.duration')}</div>
                            <div class="traffic-col-time">${t('traffic.time')}</div>
                        </div>
                        <div id="trafficLogList" class="traffic-log-list">
                            <div class="traffic-empty">${t('traffic.noLogs')}</div>
                        </div>
                    </div>
                    <!-- Logs Panel -->
                    <div class="workspace-panel" id="panel-logs">
                        <textarea id="logContent" class="log-textarea" readonly></textarea>
                    </div>
                </div>
            </div>
        </div>

        <!-- Footer -->
        <div class="footer">
            <div class="footer-content">
                <div class="footer-left">
                    <span style="opacity: 0.8;">© 2025 ccNexus</span>
                </div>
                <div class="footer-center">
                    <div class="tips-container">
                        <span id="scrollingTip" class="tip-scroll"></span>
                    </div>
                </div>
                <div class="footer-right">
                    <span style="opacity: 0.7; margin-right: 5px;">v</span>
                    <span id="appVersion" style="font-weight: 500;">1.0.0</span>
                </div>
            </div>
        </div>

        <!-- Add/Edit Endpoint Modal -->
        <div id="endpointModal" class="modal">
            <div class="modal-content">
                <div class="modal-header">
                    <h2 id="modalTitle"><span class="icon icon-lg">${getIcon('plus')}</span> ${t('modal.addEndpoint')}</h2>
                    <button class="modal-close" onclick="window.closeModal()">&times;</button>
                </div>
                <div class="modal-body">
                    <div class="form-group">
                        <label><span class="required">*</span>${t('modal.name')}</label>
                        <input type="text" id="endpointName" placeholder="${t('modal.namePlaceholder')}">
                    </div>
                    <div class="form-group">
                        <label><span class="required">*</span>${t('modal.apiUrl')}</label>
                        <input type="text" id="endpointUrl" placeholder="${t('modal.apiUrlPlaceholder')}">
                    </div>
                    <div class="form-group">
                        <label><span class="required">*</span>${t('modal.apiKey')}</label>
                        <div class="password-input-wrapper">
                            <input type="password" id="endpointKey" placeholder="${t('modal.apiKeyPlaceholder')}">
                            <button type="button" class="password-toggle" onclick="window.togglePasswordVisibility()" title="${t('modal.togglePassword')}">
                                <svg id="eyeIcon" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                    <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                                    <circle cx="12" cy="12" r="3"></circle>
                                </svg>
                            </button>
                        </div>
                    </div>
                    <div class="form-group">
                        <label><span class="required">*</span>${t('modal.transformer')}</label>
                        <select id="endpointTransformer" onchange="window.handleTransformerChange()">
                            <option value="claude">Claude (Default)</option>
                            <option value="openai">OpenAI</option>
                            <option value="openai2">OpenAI2 (Responses API)</option>
                            <option value="gemini">Gemini</option>
                            <option value="cli">Claude CLI</option>
                        </select>
                        <p style="color: #666; font-size: 12px; margin-top: 5px;">
                            ${t('modal.transformerHelp')}
                        </p>
                    </div>
                    <div class="form-group" id="modelFieldGroup" style="display: block;">
                        <label><span class="required" id="modelRequired" style="display: none;">*</span>${t('modal.model')}</label>
                        <div class="model-input-wrapper">
                            <div class="model-select-container">
                                <input type="text" id="endpointModel" placeholder="${t('modal.modelPlaceholder')}" autocomplete="off">
                                <button type="button" class="model-dropdown-toggle" onclick="window.toggleModelDropdown()">
                                    <svg width="12" height="12" viewBox="0 0 12 12" fill="currentColor">
                                        <path d="M2 4L6 8L10 4" stroke="currentColor" stroke-width="2" fill="none"/>
                                    </svg>
                                </button>
                                <div class="model-dropdown" id="modelDropdown"></div>
                            </div>
                            <button type="button" class="btn btn-secondary" id="fetchModelsBtn" onclick="window.fetchModels()" title="${t('modal.fetchModels')}">
                                <span id="fetchModelsIcon">${t('modal.fetchModelsBtn')}</span>
                            </button>
                        </div>
                        <p style="color: #666; font-size: 12px; margin-top: 5px;" id="modelHelpText">
                            ${t('modal.modelHelp')}
                        </p>
                    </div>
                    <div class="form-group">
                        <label>${t('modal.remark')}</label>
                        <input type="text" id="endpointRemark" placeholder="${t('modal.remarkHelp')}">
                    </div>
                </div>
                <div class="modal-footer">
                    <button class="btn btn-secondary" onclick="window.closeModal()">${t('modal.cancel')}</button>
                    <button class="btn btn-primary" onclick="window.saveEndpoint()">${t('modal.save')}</button>
                </div>
            </div>
        </div>

        <!-- Terminal Modal -->
        <div id="terminalModal" class="modal">
            <div class="modal-content">
                <div class="modal-header">
                    <h2><span class="icon icon-lg">${getIcon('terminal')}</span> ${t('terminal.title')}</h2>
                    <button class="modal-close" onclick="window.closeTerminalModal()">&times;</button>
                </div>
                <div class="modal-body">
                    <div class="form-group">
                        <div class="form-label-row">
                            <label><span class="required">*</span>${t('terminal.selectTerminal')}</label>
                            <div class="cli-type-switcher">
                                <button class="cli-type-btn active" data-cli="claude" onclick="window.switchCliType('claude')">Claude Code</button>
                                <button class="cli-type-btn" data-cli="codex" onclick="window.switchCliType('codex')">Codex</button>
                            </div>
                        </div>
                        <select id="terminalSelect" onchange="window.onTerminalChange()">
                            <option value="">Loading...</option>
                        </select>
                        <small class="form-help" id="terminalSelectHelp">${t('terminal.selectTerminalHelp')}</small>
                    </div>
                    <div class="form-group">
                        <label>${t('terminal.launcherCommand')}</label>
                        <input type="text" id="claudeCommandInput" placeholder="claude"
                               oninput="window.onClaudeCommandChange()">
                        <small class="form-help">${t('terminal.launcherCommandHelp')}</small>
                    </div>
                    <div class="form-group">
                        <label><span class="required">*</span>${t('terminal.projectDirs')}</label>
                        <small class="form-help">${t('terminal.projectDirsHelp')}</small>
                        <div id="projectDirList" class="project-dir-list">
                            <div class="empty-tip">${t('terminal.noDirs')}</div>
                        </div>
                    </div>
                </div>
                <div class="modal-footer">
                    <button class="btn btn-primary btn-add-dir" onclick="window.addProjectDir()">
                        <span class="icon">${getIcon('plus')}</span> ${t('terminal.addDir')}
                    </button>
                </div>
            </div>
        </div>

        <!-- Session Modal -->
        <div id="sessionModal" class="modal">
            <div class="modal-content session-modal-content">
                <div class="modal-header">
                    <h2><span class="icon icon-lg">${getIcon('clipboard')}</span> ${t('session.title')}</h2>
                    <button class="modal-close" onclick="window.closeSessionModal()">&times;</button>
                </div>
                <div class="modal-body session-modal-body">
                    <div class="session-hint">${t('session.selectHint')}</div>
                    <div id="sessionList" class="session-list">
                        <div class="session-loading">${t('session.loading')}</div>
                    </div>
                </div>
                <div class="modal-footer">
                    <button class="btn btn-primary btn-add-dir" onclick="window.confirmSessionSelection()">
                        <span class="icon">${getIcon('check')}</span> ${t('session.confirmAndReturn')}
                    </button>
                </div>
            </div>
        </div>

        <!-- Edit Port Modal -->
        <div id="portModal" class="modal">
            <div class="modal-content">
                <div class="modal-header">
                    <h2><span class="icon icon-lg">${getIcon('settings')}</span> ${t('modal.changePort')}</h2>
                    <button class="modal-close" onclick="window.closePortModal()">&times;</button>
                </div>
                <div class="modal-body">
                    <div class="form-group">
                        <label><span class="required">*</span>${t('modal.portLabel')}</label>
                        <input type="number" id="portInput" min="1" max="65535" placeholder="3000">
                    </div>
                    <p style="color: #666; font-size: 14px; margin-top: 10px;">
                        <span class="icon">${getIcon('alertTriangle')}</span> ${t('modal.portNote')}
                    </p>
                </div>
                <div class="modal-footer">
                    <button class="btn btn-secondary" onclick="window.closePortModal()">${t('modal.cancel')}</button>
                    <button class="btn btn-primary" onclick="window.savePort()">${t('modal.save')}</button>
                </div>
            </div>
        </div>

        <!-- Welcome Modal -->
        <div id="welcomeModal" class="modal">
            <div class="modal-content" style="max-width: min(600px, 90vw);">
                <div class="modal-header">
                    <h2><span class="icon">${getIcon('wave')}</span> ${t('welcome.title')}</h2>
                    <button class="modal-close" onclick="window.closeWelcomeModal()">&times;</button>
                </div>
                <div class="modal-body">
                    <p style="font-size: 16px; line-height: 1.6; margin-bottom: 20px;">
                        ${t('welcome.message')}
                    </p>

                    <div style="display: flex; justify-content: center; gap: 30px; margin: 30px 0;">
                        <div style="text-align: center;">
                            <img src="/WeChat.jpg" alt="WeChat QR Code" style="width: 200px; height: 200px; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,0.1);">
                            <p style="margin-top: 10px; color: #666; font-size: 14px;">${t('welcome.qrCodeTip')}</p>
                        </div>
                        <div style="text-align: center;">
                            <img
                                id="chatQRCodeImg"
                                src="/ME.png"
                                alt="Chat Group QR Code"
                                style="width: 200px; height: 200px; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,0.1);"
                            >
                            <p id="chatQRCodeTip" style="margin-top: 10px; color: #666; font-size: 14px;">${t('welcome.chatGroupFallbackTip')}</p>
                        </div>
                    </div>

                    <div style="display: flex; gap: 15px; justify-content: center; margin-top: 20px;">
                        <button class="btn btn-secondary" onclick="window.openArticle()">
                            <span class="icon">${getIcon('book')}</span> ${t('welcome.readArticle')}
                        </button>
                        <button class="btn btn-secondary" onclick="window.showChangelogModal()">
                            <span class="icon">${getIcon('list')}</span> ${t('welcome.changelog')}
                        </button>
                        <button class="btn btn-secondary check-update-btn" onclick="window.checkForUpdates()">
                            <span class="icon">${getIcon('refresh')}</span> ${t('update.checkForUpdates')}
                            <span class="update-badge" id="checkUpdateBadge"></span>
                        </button>
                    </div>
                </div>
                <div class="modal-footer" style="display: flex; justify-content: flex-end; align-items: center; gap: 20px;">
                    <label style="display: flex; align-items: center; cursor: pointer;">
                        <input type="checkbox" id="dontShowAgain" style="margin-right: 8px;">
                        <span style="font-size: 14px; color: #666;">${t('welcome.dontShow')}</span>
                    </label>
                    <button class="btn btn-primary" onclick="window.closeWelcomeModal()">${t('welcome.getStarted')}</button>
                </div>
            </div>
        </div>

        <!-- Test Result Modal -->
        <div id="testResultModal" class="modal">
            <div class="modal-content" style="max-width: min(600px, 90vw);">
                <div class="modal-header">
                    <h2 id="testResultTitle"><span class="icon icon-lg">${getIcon('test')}</span> ${t('test.title')}</h2>
                    <button class="modal-close" onclick="window.closeTestResultModal()">&times;</button>
                </div>
                <div class="modal-body">
                    <div id="testResultContent" style="font-size: 14px; line-height: 1.6;">
                        <!-- Test result will be inserted here -->
                    </div>
                </div>
            </div>
        </div>

        <!-- Changelog Modal -->
        <div id="changelogModal" class="modal">
            <div class="modal-content">
                <div class="modal-header">
                    <h2><span class="icon icon-lg">${getIcon('clipboard')}</span> ${t('changelog.title')}</h2>
                    <button class="modal-close" onclick="window.closeChangelogModal()">&times;</button>
                </div>
                <div class="modal-body">
                    <div id="changelogContent" style="font-size: 14px; line-height: 1.8;">
                    </div>
                </div>
            </div>
        </div>

        <!-- Error Toast -->
        <div id="errorToast" class="error-toast">
            <div class="error-toast-content">
                <span class="error-toast-icon">${getIcon('alertTriangle')}</span>
                <span id="errorToastMessage"></span>
            </div>
        </div>

        <!-- Confirm Dialog -->
        <div id="confirmDialog" class="modal">
            <div class="confirm-dialog-content">
                <div class="confirm-body">
                    <div class="confirm-icon">
                        <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                            <path d="M12 9v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                        </svg>
                    </div>
                    <div class="confirm-content">
                        <h4 class="confirm-title">${t('common.confirmDeleteTitle')}</h4>
                        <p id="confirmMessage" class="confirm-message"></p>
                    </div>
                </div>
                <div class="confirm-divider"></div>
                <div class="confirm-footer">
                    <button class="btn-confirm-delete" onclick="window.acceptConfirm()">${t('common.delete')}</button>
                    <button class="btn-confirm-cancel" onclick="window.cancelConfirm()">${t('common.cancel')}</button>
                </div>
            </div>
        </div>

        <!-- Close Action Dialog -->
        <div id="closeActionDialog" class="modal">
            <div class="confirm-dialog-content">
                <div class="confirm-body">
                    <div class="confirm-icon" style="background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);">
                        <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                            <path d="M6 18L18 6M6 6l12 12" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                        </svg>
                    </div>
                    <div class="confirm-content">
                        <h4 class="confirm-title">关闭窗口</h4>
                        <p class="confirm-message">您希望如何处理？</p>
                    </div>
                </div>
                <div class="confirm-divider"></div>
                <div class="confirm-footer">
                    <button class="btn-confirm-delete" onclick="window.quitApplication()">退出程序</button>
                    <button class="btn-confirm-cancel" onclick="window.minimizeToTray()">最小化到托盘</button>
                </div>
            </div>
        </div>

        <!-- Settings Modal -->
        <div id="settingsModal" class="modal">
            <div class="modal-content">
                <div class="modal-header">
                    <h2><span class="icon icon-lg">${getIcon('settings')}</span> ${t('settings.title')}</h2>
                    <button class="modal-close" onclick="window.closeSettingsModal()">&times;</button>
                </div>
                <div class="modal-body">
                    <div class="form-group">
                        <label><span class="required">*</span>${t('settings.language')}</label>
                        <select id="settingsLanguage">
                            <option value="zh-CN">${t('settings.languages.zh-CN')}</option>
                            <option value="en">${t('settings.languages.en')}</option>
                        </select>
                        <p style="color: #666; font-size: 12px; margin-top: 5px;">
                            ${t('settings.languageHelp')}
                        </p>
                    </div>
                    <div class="form-group">
                        <label><span class="required">*</span>${t('settings.theme')}</label>
                        <div style="display: flex; align-items: center; gap: 12px;">
                            <select id="settingsTheme" style="flex: 1;">
                                <option value="light">${t('settings.themes.light')}</option>
                                <option value="dark">${t('settings.themes.dark')}</option>
                                <option value="green">${t('settings.themes.green')}</option>
                                <option value="starry">${t('settings.themes.starry')}</option>
                                <option value="sakura">${t('settings.themes.sakura')}</option>
                                <option value="sunset">${t('settings.themes.sunset')}</option>
                                <option value="ocean">${t('settings.themes.ocean')}</option>
                                <option value="mocha">${t('settings.themes.mocha')}</option>
                                <option value="cyberpunk">${t('settings.themes.cyberpunk')}</option>
                                <option value="aurora">${t('settings.themes.aurora')}</option>
                                <option value="holographic">${t('settings.themes.holographic')}</option>
                                <option value="quantum">${t('settings.themes.quantum')}</option>
                            </select>
                            <div style="display: flex; align-items: center; gap: 8px; white-space: nowrap;" title="${t('settings.themeAutoHelp')}">
                                <span style="font-size: 13px; color: var(--text-secondary);">${t('settings.themeAuto')}</span>
                                <label class="toggle-switch" style="width: 40px; height: 20px; margin-top: 7px;">
                                    <input type="checkbox" id="settingsThemeAuto">
                                    <span class="toggle-slider" style="border-radius: 20px;"></span>
                                </label>
                            </div>
                        </div>
                        <p style="color: #666; font-size: 12px; margin-top: 5px;">
                            ${t('settings.themeHelp')}
                        </p>
                    </div>
                    <div class="form-group">
                        <label><span class="required">*</span>${t('settings.claudeNotification')}</label>
                        <select id="settingsNotificationType">
                            <option value="disabled">${t('settings.notificationOptions.disabled')}</option>
                            <option value="toast">${t('settings.notificationOptions.toast')}</option>
                            <option value="dialog">${t('settings.notificationOptions.dialog')}</option>
                        </select>
                        <p style="color: #666; font-size: 12px; margin-top: 5px;">
                            ${t('settings.notificationHelp')}
                        </p>
                    </div>
                    <div class="form-group">
                        <label><span class="required">*</span>${t('settings.closeWindowBehavior')}</label>
                        <select id="settingsCloseWindowBehavior">
                            <option value="quit">${t('settings.closeWindowOptions.quit')}</option>
                            <option value="ask">${t('settings.closeWindowOptions.ask')}</option>
                            <option value="minimize">${t('settings.closeWindowOptions.minimize')}</option>
                        </select>
                        <p style="color: #666; font-size: 12px; margin-top: 5px;">
                            ${t('settings.closeWindowBehaviorHelp')}
                        </p>
                    </div>
                    <div class="form-group">
                        <label>${t('settings.proxy')}</label>
                        <input type="text" id="settingsProxyUrl" placeholder="${t('settings.proxyUrlPlaceholder')}">
                        <p style="color: #666; font-size: 12px; margin-top: 5px;">
                            ${t('settings.proxyHelp')}
                        </p>
                    </div>
                    <div class="form-group">
                        <label><span class="required">*</span>${t('update.autoCheck')}</label>
                        <select id="check-interval">
                            <option value="1">${t('update.everyHour')}</option>
                            <option value="24">${t('update.everyDay')}</option>
                            <option value="168">${t('update.everyWeek')}</option>
                            <option value="720">${t('update.everyMonth')}</option>
                            <option value="0">${t('update.noAutoCheck')}</option>
                        </select>
                        <p style="color: #666; font-size: 12px; margin-top: 5px;">
                            ${t('update.autoCheckHelp')}
                        </p>
                    </div>
                </div>
                <div class="modal-footer">
                    <button class="btn btn-secondary" onclick="window.closeSettingsModal()">${t('settings.cancel')}</button>
                    <button class="btn btn-primary" onclick="window.saveSettings()">${t('settings.save')}</button>
                </div>
            </div>
        </div>

        <!-- Auto Theme Config Modal -->
        <div id="autoThemeConfigModal" class="modal">
            <div class="modal-content">
                <div class="modal-header">
                    <h2><span class="icon icon-lg">${getIcon('moon')}</span> ${t('settings.autoThemeConfigTitle')}</h2>
                    <button class="modal-close" onclick="window.closeAutoThemeConfigModal()">&times;</button>
                </div>
                <div class="modal-body">
                    <p style="color: var(--text-secondary); font-size: 14px; margin-bottom: 20px;">
                        ${t('settings.autoThemeConfigDesc')}
                    </p>
                    <div class="form-group">
                        <label><span class="required">*</span>${t('settings.lightThemeLabel')}</label>
                        <select id="autoLightTheme">
                            <option value="light">${t('settings.themes.light')}</option>
                            <option value="green">${t('settings.themes.green')}</option>
                            <option value="sakura">${t('settings.themes.sakura')}</option>
                            <option value="sunset">${t('settings.themes.sunset')}</option>
                            <option value="ocean">${t('settings.themes.ocean')}</option>
                            <option value="mocha">${t('settings.themes.mocha')}</option>
                        </select>
                        <p style="color: var(--text-secondary); font-size: 12px; margin-top: 5px;">
                            ${t('settings.lightThemeHelp')}
                        </p>
                    </div>
                    <div class="form-group">
                        <label><span class="required">*</span>${t('settings.darkThemeLabel')}</label>
                        <select id="autoDarkTheme">
                            <option value="dark">${t('settings.themes.dark')}</option>
                            <option value="starry">${t('settings.themes.starry')}</option>
                            <option value="cyberpunk">${t('settings.themes.cyberpunk')}</option>
                            <option value="aurora">${t('settings.themes.aurora')}</option>
                            <option value="holographic">${t('settings.themes.holographic')}</option>
                            <option value="quantum">${t('settings.themes.quantum')}</option>
                        </select>
                        <p style="color: var(--text-secondary); font-size: 12px; margin-top: 5px;">
                            ${t('settings.darkThemeHelp')}
                        </p>
                    </div>
                </div>
                <div class="modal-footer">
                    <button class="btn btn-secondary" onclick="window.closeAutoThemeConfigModal()">${t('settings.cancel')}</button>
                    <button class="btn btn-primary" onclick="window.saveAutoThemeConfig()">${t('settings.save')}</button>
                </div>
            </div>
        </div>

        <!-- Sponsor Modal -->
        <div id="sponsorModal" class="modal">
            <div class="modal-content sponsor-modal-content">
                <div class="modal-header">
                    <h2><span class="icon icon-lg">${getIcon('heart')}</span> ${t('sponsor.title')}</h2>
                    <button class="modal-close" onclick="window.closeSponsorModal()">&times;</button>
                </div>
                <div class="modal-body">
                    <div class="sponsor-grid"></div>
                </div>
            </div>
        </div>

        <!-- Traffic Detail Modal -->
        <div id="trafficDetailModal" class="modal">
            <div class="modal-content traffic-detail-modal">
                <div class="modal-header">
                    <h2><span class="icon">${getIcon('satellite')}</span> ${t('traffic.detailTitle')}</h2>
                    <button class="modal-close" onclick="window.closeTrafficDetailModal()">&times;</button>
                </div>
                <div class="modal-body" id="trafficDetailContent">
                    <!-- Traffic detail will be inserted here -->
                </div>
            </div>
        </div>
    `;

    setupModalEventListeners();
}

function setupModalEventListeners() {
    // Close modals on background click (endpointModal, portModal, welcomeModal do NOT close on background click)
     document.getElementById('testResultModal').addEventListener('click', (e) => {
        if (e.target.id === 'testResultModal') {
            window.closeTestResultModal();
        }
    });

    // Close traffic detail modal on background click
    document.getElementById('trafficDetailModal').addEventListener('click', (e) => {
        if (e.target.id === 'trafficDetailModal') {
            window.closeTrafficDetailModal();
        }
    });
}

export async function changeLanguage(lang) {
    try {
        await window.go.main.App.SetLanguage(lang);
        location.reload();
    } catch (error) {
        console.error('Failed to change language:', error);
    }
}
