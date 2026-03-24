import { router } from './router.js';
import { state } from './state.js';
import { dashboard } from './components/dashboard.js';
import { endpoints } from './components/endpoints.js';
import { stats } from './components/stats.js';
import { testing } from './components/testing.js';
import { traffic } from './components/traffic.js';
import { users } from './components/users.js';
import { icons, getIcon } from './icons.js';
import { api, AUTH_TOKEN_KEY, CURRENT_USER_KEY } from './api.js';

// Populate all data-icon elements
function initIcons() {
    document.querySelectorAll('[data-icon]').forEach(el => {
        el.innerHTML = getIcon(el.dataset.icon);
    });
}

// Initialize theme
function initTheme() {
    const savedTheme = localStorage.getItem('theme') || 'light';
    document.body.classList.toggle('dark-theme', savedTheme === 'dark');

    const themeToggle = document.getElementById('theme-toggle');
    themeToggle.addEventListener('click', () => {
        const isDark = document.body.classList.toggle('dark-theme');
        localStorage.setItem('theme', isDark ? 'dark' : 'light');
        themeToggle.querySelector('.icon').innerHTML = isDark ? getIcon('sun') : getIcon('moon');
    });

    // Set initial icon
    themeToggle.querySelector('.icon').innerHTML = savedTheme === 'dark' ? getIcon('sun') : getIcon('moon');
}

// Initialize real-time updates (close old connection before reconnect)
let eventSourceInstance = null;
let sseRetryCount = 0;
const SSE_MAX_RETRIES = 10;
const SSE_BASE_DELAY = 2000;

function initRealtime() {
    if (eventSourceInstance) {
        eventSourceInstance.close();
        eventSourceInstance = null;
    }
    const token = sessionStorage.getItem(AUTH_TOKEN_KEY);
    if (!token) {
        return;
    }
    const url = `/api/events?token=${encodeURIComponent(token)}`;
    eventSourceInstance = new EventSource(url);

    eventSourceInstance.onopen = () => {
        sseRetryCount = 0;
    };

    eventSourceInstance.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);

            if (data.type === 'stats') {
                state.update('stats', data.stats);
                state.update('currentEndpoint', data.currentEndpoint);
            }
        } catch (error) {
            console.error('Failed to parse SSE event:', error);
        }
    };

    eventSourceInstance.onerror = () => {
        eventSourceInstance.close();
        eventSourceInstance = null;

        if (sseRetryCount >= SSE_MAX_RETRIES) {
            console.warn('SSE: max retries reached, stopping reconnection');
            return;
        }

        const delay = Math.min(SSE_BASE_DELAY * Math.pow(2, sseRetryCount), 60000);
        sseRetryCount++;
        console.warn(`SSE: reconnecting in ${delay}ms (attempt ${sseRetryCount}/${SSE_MAX_RETRIES})`);
        setTimeout(initRealtime, delay);
    };
}

// Show login form when auth required
function showLoginForm() {
    const app = document.getElementById('app');
    sessionStorage.removeItem(CURRENT_USER_KEY);
    state.update('currentUser', null);
    app.innerHTML = `
        <div class="login-overlay">
            <div class="login-box">
                <h1>ccNexus</h1>
                <p class="login-hint">管理员 Token 已固定，请直接输入。</p>
                <form id="login-form">
                    <input type="password" id="login-token" placeholder="API Token" autocomplete="off" />
                    <button type="submit">进入</button>
                </form>
                <p id="login-error" class="login-error"></p>
            </div>
        </div>
    `;
    document.getElementById('login-form').addEventListener('submit', async (e) => {
        e.preventDefault();
        const token = document.getElementById('login-token').value.trim();
        const errEl = document.getElementById('login-error');
        errEl.textContent = '';
        if (!token) {
            errEl.textContent = '请输入 Token';
            return;
        }
        try {
            const res = await api.verifyToken(token);
            if (res.ok && (res.success || res.data)) {
                const user = res.data?.user || res.user || null;
                sessionStorage.setItem(AUTH_TOKEN_KEY, token);
                if (user) {
                    sessionStorage.setItem(CURRENT_USER_KEY, JSON.stringify(user));
                }
                location.reload();
            } else {
                errEl.textContent = res.error || 'Token 无效';
            }
        } catch (err) {
            errEl.textContent = err.message || '验证失败';
        }
    });
}

// Initialize application
async function init() {
    // Prevent transitions on page load
    document.body.classList.add('preload');

    let currentUser = null;
    const token = sessionStorage.getItem(AUTH_TOKEN_KEY);
    const cachedUser = sessionStorage.getItem(CURRENT_USER_KEY);
    if (cachedUser) {
        try {
            currentUser = JSON.parse(cachedUser);
        } catch {
            sessionStorage.removeItem(CURRENT_USER_KEY);
        }
    }

    // Check auth
    try {
        const status = await api.getAuthStatus();
        if (status.authRequired && !token) {
            showLoginForm();
            return;
        }
        if (token) {
            const verify = await api.verifyToken(token);
            if (!verify.ok) {
                sessionStorage.removeItem(AUTH_TOKEN_KEY);
                sessionStorage.removeItem(CURRENT_USER_KEY);
                showLoginForm();
                return;
            }
            currentUser = verify.data?.user || verify.user || currentUser;
            if (currentUser) {
                sessionStorage.setItem(CURRENT_USER_KEY, JSON.stringify(currentUser));
                state.update('currentUser', currentUser);
            }
        }
    } catch (err) {
        console.warn('Auth check failed:', err);
    }

    const usersNavItem = document.getElementById('users-nav-item');
    const isAdmin = currentUser?.role === 'admin';
    if (usersNavItem) {
        usersNavItem.style.display = isAdmin ? '' : 'none';
    }

    // Register routes
    router.register('dashboard', dashboard);
    router.register('endpoints', endpoints);
    router.register('stats', stats);
    router.register('testing', testing);
    router.register('traffic', traffic);
    if (isAdmin) {
        router.register('users', users);
    }

    // Initialize icons
    initIcons();

    // Initialize theme
    initTheme();

    // Add logout button when token auth is used
    if (sessionStorage.getItem(AUTH_TOKEN_KEY)) {
        const footer = document.querySelector('.sidebar-footer');
        if (footer) {
            const logoutBtn = document.createElement('button');
            logoutBtn.className = 'btn-icon';
            logoutBtn.title = '退出登录';
            logoutBtn.innerHTML = '<span class="icon" data-icon="logOut"></span>';
            logoutBtn.addEventListener('click', () => {
                sessionStorage.removeItem(AUTH_TOKEN_KEY);
                sessionStorage.removeItem(CURRENT_USER_KEY);
                location.reload();
            });
            footer.insertBefore(logoutBtn, footer.firstChild);
            initIcons();
        }
    }

    if (!isAdmin && state.get('currentView') === 'users') {
        state.update('currentView', 'dashboard');
    }

    // Initialize router
    router.init();

    // Initialize real-time updates
    initRealtime();

    // Remove preload class after initialization
    setTimeout(() => {
        document.body.classList.remove('preload');
    }, 100);

    console.log('ccNexus Admin initialized');
}

// Start application when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
} else {
    init();
}
