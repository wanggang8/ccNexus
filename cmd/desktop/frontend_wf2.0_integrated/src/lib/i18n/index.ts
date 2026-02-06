import { writable, derived } from 'svelte/store';

type Translations = Record<string, string>;

const translations: Record<string, Translations> = {
  en: {
    'app.title': 'ccNexus',
    'header.title': 'API Gateway for Claude Code',
    'header.port': 'Port',
    'header.githubRepo': 'GitHub Repository',
    'header.about': 'About',
    'settings.title': 'Settings',
    'nav.dashboard': 'Dashboard',
    'nav.endpoints': 'Endpoints',
    'nav.sessions': 'Sessions',
    'nav.stats': 'Statistics',
    'nav.logs': 'Logs',
    'nav.settings': 'Settings',
    'dashboard.welcome': 'Welcome to ccNexus 2.0',
    'dashboard.description': 'Manage your API endpoints and monitor usage',
    'endpoints.title': 'Endpoints',
    'endpoints.add': 'Add Endpoint',
    'endpoints.empty': 'No endpoints configured',
    'sessions.title': 'Sessions',
    'stats.title': 'Statistics',
    'logs.title': 'Logs',
  },
  'zh-CN': {
    'app.title': 'ccNexus',
    'header.title': 'Claude Code API 网关',
    'header.port': '端口',
    'header.githubRepo': 'GitHub 仓库',
    'header.about': '关于',
    'settings.title': '设置',
    'nav.dashboard': '仪表盘',
    'nav.endpoints': '端点管理',
    'nav.sessions': '会话',
    'nav.stats': '统计',
    'nav.logs': '日志',
    'nav.settings': '设置',
    'dashboard.welcome': '欢迎使用 ccNexus 2.0',
    'dashboard.description': '管理您的 API 端点并监控使用情况',
    'endpoints.title': '端点管理',
    'endpoints.add': '添加端点',
    'endpoints.empty': '暂无端点配置',
    'sessions.title': '会话',
    'stats.title': '统计',
    'logs.title': '日志',
  },
};

export const locale = writable<string>('zh-CN');

export const t = derived(locale, ($locale) => {
  return (key: string): string => {
    return translations[$locale]?.[key] || translations['en']?.[key] || key;
  };
});

export function setLocale(newLocale: string) {
  locale.set(newLocale);
}
