declare global {
  interface Window {
    go: {
      main: {
        App: {
          // Endpoint methods
          AddEndpoint(name: string, apiUrl: string, apiKey: string, transformer: string, model: string, remark: string): Promise<void>;
          RemoveEndpoint(index: number): Promise<void>;
          UpdateEndpoint(index: number, name: string, apiUrl: string, apiKey: string, transformer: string, model: string, remark: string): Promise<void>;
          ToggleEndpoint(index: number, enabled: boolean): Promise<void>;
          ReorderEndpoints(names: string[]): Promise<void>;
          GetCurrentEndpoint(): Promise<string>;
          SwitchToEndpoint(endpointName: string): Promise<void>;
          TestEndpoint(index: number): Promise<string>;
          TestEndpointLight(index: number): Promise<string>;
          TestAllEndpointsZeroCost(): Promise<string>;
          FetchModels(apiUrl: string, apiKey: string, transformer: string): Promise<string>;

          // Config methods
          GetConfig(): Promise<string>;
          GetProxyPort(): Promise<number>;
          SetProxyPort(port: number): Promise<void>;

          // Stats methods
          GetDailyStats(): Promise<string>;
          GetWeeklyStats(): Promise<string>;
          GetMonthlyStats(): Promise<string>;
          GetYesterdayStats(): Promise<string>;
          GetHistoricalStats(): Promise<string>;
          GetStatsTrend(): Promise<string>;
          GetStatsTrendByPeriod(period: string): Promise<string>;

          // Settings methods
          GetSettings(): Promise<string>;
          UpdateSettings(settings: string): Promise<void>;
          GetTheme(): Promise<string>;
          SetTheme(theme: string): Promise<void>;
          GetThemeAuto(): Promise<boolean>;
          SetThemeAuto(auto: boolean): Promise<void>;
          GetAutoLightTheme(): Promise<string>;
          SetAutoLightTheme(theme: string): Promise<void>;
          GetAutoDarkTheme(): Promise<string>;
          SetAutoDarkTheme(theme: string): Promise<void>;
          GetLanguage(): Promise<string>;
          SetLanguage(lang: string): Promise<void>;
          GetChangelog(lang: string): Promise<string>;

          // Terminal methods
          OpenTerminal(): Promise<void>;

          // Log methods
          GetLogs(): Promise<string>;
          GetLogsByLevel(level: number): Promise<string>;
          ClearLogs(): Promise<void>;
          SetLogLevel(level: number): Promise<void>;
          GetLogLevel(): Promise<number>;

          // WebDAV methods
          UpdateWebDAVConfig(url: string, username: string, password: string): Promise<void>;
          TestWebDAVConnection(url: string, username: string, password: string): Promise<string>;
          BackupToWebDAV(filename: string): Promise<void>;
          RestoreFromWebDAV(filename: string, choice: string): Promise<void>;
          ListWebDAVBackups(): Promise<string>;

          // Session methods
          GetSessions(projectDir: string): Promise<string>;
          DeleteSession(projectDir: string, sessionId: string): Promise<void>;
          RenameSession(projectDir: string, sessionId: string, alias: string): Promise<void>;
          GetSessionData(projectDir: string, sessionId: string): Promise<string>;
          LaunchSessionTerminal(dir: string, sessionId: string): Promise<void>;

          // Version
          GetVersion(): Promise<string>;
        };
      };
    };
  }
}

export interface Endpoint {
  name: string;
  apiUrl: string;
  apiKey: string;
  transformer: string;
  model: string;
  remark: string;
  enabled: boolean;
}

export interface Config {
  proxyPort: number;
  endpoints: Endpoint[];
}

export interface Stats {
  requests: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens?: number;
  cacheWriteTokens?: number;
}

// Check if running in Wails environment
export function isWailsEnv(): boolean {
  return typeof window !== 'undefined' && window.go?.main?.App !== undefined;
}

// Endpoint API
export const endpointApi = {
  async getConfig(): Promise<Config | null> {
    if (!isWailsEnv()) return null;
    try {
      const configStr = await window.go.main.App.GetConfig();
      return JSON.parse(configStr);
    } catch (e) {
      console.error('Failed to get config:', e);
      return null;
    }
  },

  async addEndpoint(endpoint: Omit<Endpoint, 'enabled'>): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.AddEndpoint(
        endpoint.name,
        endpoint.apiUrl,
        endpoint.apiKey,
        endpoint.transformer,
        endpoint.model,
        endpoint.remark || ''
      );
      return true;
    } catch (e) {
      console.error('Failed to add endpoint:', e);
      return false;
    }
  },

  async updateEndpoint(index: number, endpoint: Omit<Endpoint, 'enabled'>): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.UpdateEndpoint(
        index,
        endpoint.name,
        endpoint.apiUrl,
        endpoint.apiKey,
        endpoint.transformer,
        endpoint.model,
        endpoint.remark || ''
      );
      return true;
    } catch (e) {
      console.error('Failed to update endpoint:', e);
      return false;
    }
  },

  async removeEndpoint(index: number): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.RemoveEndpoint(index);
      return true;
    } catch (e) {
      console.error('Failed to remove endpoint:', e);
      return false;
    }
  },

  async toggleEndpoint(index: number, enabled: boolean): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.ToggleEndpoint(index, enabled);
      return true;
    } catch (e) {
      console.error('Failed to toggle endpoint:', e);
      return false;
    }
  },

  async testEndpoint(index: number): Promise<{ success: boolean; message: string }> {
    if (!isWailsEnv()) return { success: false, message: 'Not in Wails environment' };
    try {
      const result = await window.go.main.App.TestEndpoint(index);
      const parsed = JSON.parse(result);
      return { success: parsed.success, message: parsed.message || '' };
    } catch (e) {
      return { success: false, message: String(e) };
    }
  },

  async reorderEndpoints(names: string[]): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.ReorderEndpoints(names);
      return true;
    } catch (e) {
      console.error('Failed to reorder endpoints:', e);
      return false;
    }
  },

  async testAllEndpoints(): Promise<{ success: boolean; results: any[] }> {
    if (!isWailsEnv()) return { success: false, results: [] };
    try {
      const result = await window.go.main.App.TestAllEndpointsZeroCost();
      const parsed = JSON.parse(result);
      return { success: true, results: parsed };
    } catch (e) {
      return { success: false, results: [] };
    }
  },

  async fetchModels(apiUrl: string, apiKey: string, transformer: string): Promise<string[]> {
    if (!isWailsEnv()) return [];
    try {
      const result = await window.go.main.App.FetchModels(apiUrl, apiKey, transformer);
      const parsed = JSON.parse(result);
      return parsed.models || [];
    } catch (e) {
      console.error('Failed to fetch models:', e);
      return [];
    }
  },

  async getCurrentEndpoint(): Promise<string> {
    if (!isWailsEnv()) return '';
    try {
      return await window.go.main.App.GetCurrentEndpoint();
    } catch (e) {
      console.error('Failed to get current endpoint:', e);
      return '';
    }
  },

  async switchToEndpoint(name: string): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.SwitchToEndpoint(name);
      return true;
    } catch (e) {
      console.error('Failed to switch endpoint:', e);
      return false;
    }
  },
};

// Stats API
export const statsApi = {
  async getDailyStats(): Promise<Stats | null> {
    if (!isWailsEnv()) return null;
    try {
      const result = await window.go.main.App.GetDailyStats();
      return JSON.parse(result);
    } catch (e) {
      console.error('Failed to get daily stats:', e);
      return null;
    }
  },

  async getWeeklyStats(): Promise<Stats | null> {
    if (!isWailsEnv()) return null;
    try {
      const result = await window.go.main.App.GetWeeklyStats();
      return JSON.parse(result);
    } catch (e) {
      console.error('Failed to get weekly stats:', e);
      return null;
    }
  },

  async getMonthlyStats(): Promise<Stats | null> {
    if (!isWailsEnv()) return null;
    try {
      const result = await window.go.main.App.GetMonthlyStats();
      return JSON.parse(result);
    } catch (e) {
      console.error('Failed to get monthly stats:', e);
      return null;
    }
  },

  async getStatsTrend(): Promise<any[]> {
    if (!isWailsEnv()) return [];
    try {
      const result = await window.go.main.App.GetStatsTrend();
      return JSON.parse(result) || [];
    } catch (e) {
      console.error('Failed to get stats trend:', e);
      return [];
    }
  },

  async getStatsTrendByPeriod(period: string): Promise<any[]> {
    if (!isWailsEnv()) return [];
    try {
      const result = await window.go.main.App.GetStatsTrendByPeriod(period);
      return JSON.parse(result) || [];
    } catch (e) {
      console.error('Failed to get stats trend by period:', e);
      return [];
    }
  },
};

// Settings API
export const settingsApi = {
  async getProxyPort(): Promise<number> {
    if (!isWailsEnv()) return 3000;
    try {
      return await window.go.main.App.GetProxyPort();
    } catch (e) {
      console.error('Failed to get proxy port:', e);
      return 3000;
    }
  },

  async setProxyPort(port: number): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.SetProxyPort(port);
      return true;
    } catch (e) {
      console.error('Failed to set proxy port:', e);
      return false;
    }
  },

  async getTheme(): Promise<string> {
    if (!isWailsEnv()) return 'light';
    try {
      return await window.go.main.App.GetTheme();
    } catch (e) {
      console.error('Failed to get theme:', e);
      return 'light';
    }
  },

  async setTheme(theme: string): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.SetTheme(theme);
      return true;
    } catch (e) {
      console.error('Failed to set theme:', e);
      return false;
    }
  },

  async getLanguage(): Promise<string> {
    if (!isWailsEnv()) return 'zh-CN';
    try {
      return await window.go.main.App.GetLanguage();
    } catch (e) {
      console.error('Failed to get language:', e);
      return 'zh-CN';
    }
  },

  async setLanguage(lang: string): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.SetLanguage(lang);
      return true;
    } catch (e) {
      console.error('Failed to set language:', e);
      return false;
    }
  },

  async getThemeAuto(): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      return await window.go.main.App.GetThemeAuto();
    } catch (e) {
      console.error('Failed to get theme auto:', e);
      return false;
    }
  },

  async setThemeAuto(auto: boolean): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.SetThemeAuto(auto);
      return true;
    } catch (e) {
      console.error('Failed to set theme auto:', e);
      return false;
    }
  },

  async getChangelog(lang: string): Promise<string> {
    if (!isWailsEnv()) return '';
    try {
      return await window.go.main.App.GetChangelog(lang);
    } catch (e) {
      console.error('Failed to get changelog:', e);
      return '';
    }
  },
};

// Terminal API
export const terminalApi = {
  async openTerminal(): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.OpenTerminal();
      return true;
    } catch (e) {
      console.error('Failed to open terminal:', e);
      return false;
    }
  },
};

// Session API
export const sessionApi = {
  async getSessions(projectDir: string): Promise<any[]> {
    if (!isWailsEnv()) return [];
    try {
      const result = await window.go.main.App.GetSessions(projectDir);
      return JSON.parse(result) || [];
    } catch (e) {
      console.error('Failed to get sessions:', e);
      return [];
    }
  },

  async deleteSession(projectDir: string, sessionId: string): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.DeleteSession(projectDir, sessionId);
      return true;
    } catch (e) {
      console.error('Failed to delete session:', e);
      return false;
    }
  },

  async renameSession(projectDir: string, sessionId: string, alias: string): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.RenameSession(projectDir, sessionId, alias);
      return true;
    } catch (e) {
      console.error('Failed to rename session:', e);
      return false;
    }
  },

  async getSessionData(projectDir: string, sessionId: string): Promise<any> {
    if (!isWailsEnv()) return null;
    try {
      const result = await window.go.main.App.GetSessionData(projectDir, sessionId);
      return JSON.parse(result);
    } catch (e) {
      console.error('Failed to get session data:', e);
      return null;
    }
  },

  async launchSessionTerminal(dir: string, sessionId: string): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.LaunchSessionTerminal(dir, sessionId);
      return true;
    } catch (e) {
      console.error('Failed to launch session terminal:', e);
      return false;
    }
  },
};

// Log API
export const logApi = {
  async getLogs(): Promise<string[]> {
    if (!isWailsEnv()) return [];
    try {
      const result = await window.go.main.App.GetLogs();
      return JSON.parse(result) || [];
    } catch (e) {
      console.error('Failed to get logs:', e);
      return [];
    }
  },

  async getLogsByLevel(level: number): Promise<string[]> {
    if (!isWailsEnv()) return [];
    try {
      const result = await window.go.main.App.GetLogsByLevel(level);
      return JSON.parse(result) || [];
    } catch (e) {
      console.error('Failed to get logs by level:', e);
      return [];
    }
  },

  async clearLogs(): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.ClearLogs();
      return true;
    } catch (e) {
      console.error('Failed to clear logs:', e);
      return false;
    }
  },

  async setLogLevel(level: number): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.SetLogLevel(level);
      return true;
    } catch (e) {
      console.error('Failed to set log level:', e);
      return false;
    }
  },

  async getLogLevel(): Promise<number> {
    if (!isWailsEnv()) return 0;
    try {
      return await window.go.main.App.GetLogLevel();
    } catch (e) {
      console.error('Failed to get log level:', e);
      return 0;
    }
  },
};

// WebDAV API
export const webdavApi = {
  async updateConfig(url: string, username: string, password: string): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.UpdateWebDAVConfig(url, username, password);
      return true;
    } catch (e) {
      console.error('Failed to update WebDAV config:', e);
      return false;
    }
  },

  async testConnection(url: string, username: string, password: string): Promise<{ success: boolean; message: string }> {
    if (!isWailsEnv()) return { success: false, message: 'Not in Wails environment' };
    try {
      const result = await window.go.main.App.TestWebDAVConnection(url, username, password);
      const parsed = JSON.parse(result);
      return { success: parsed.success, message: parsed.message || '' };
    } catch (e) {
      return { success: false, message: String(e) };
    }
  },

  async backup(filename: string): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.BackupToWebDAV(filename);
      return true;
    } catch (e) {
      console.error('Failed to backup to WebDAV:', e);
      return false;
    }
  },

  async restore(filename: string, choice: string): Promise<boolean> {
    if (!isWailsEnv()) return false;
    try {
      await window.go.main.App.RestoreFromWebDAV(filename, choice);
      return true;
    } catch (e) {
      console.error('Failed to restore from WebDAV:', e);
      return false;
    }
  },

  async listBackups(): Promise<string[]> {
    if (!isWailsEnv()) return [];
    try {
      const result = await window.go.main.App.ListWebDAVBackups();
      return JSON.parse(result) || [];
    } catch (e) {
      console.error('Failed to list WebDAV backups:', e);
      return [];
    }
  },
};

// Version API
export const versionApi = {
  async getVersion(): Promise<string> {
    if (!isWailsEnv()) return '2.0.0-dev';
    try {
      return await window.go.main.App.GetVersion();
    } catch (e) {
      console.error('Failed to get version:', e);
      return '2.0.0-dev';
    }
  },
};
