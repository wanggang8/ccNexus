/**
 * UI 2.0 功能清单 - 与 1.0 对比
 * 
 * 此文件用于记录和验证 UI 2.0 是否完整实现了 1.0 的所有功能
 */

export interface FeatureItem {
  name: string;
  category: string;
  v1Status: 'implemented' | 'not-implemented';
  v2Status: 'implemented' | 'partial' | 'not-implemented';
  apiMethod?: string;
  notes?: string;
}

export const featureChecklist: FeatureItem[] = [
  // ==================== 端点管理 ====================
  {
    name: '添加端点',
    category: 'endpoints',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'AddEndpoint',
  },
  {
    name: '删除端点',
    category: 'endpoints',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'RemoveEndpoint',
  },
  {
    name: '编辑端点',
    category: 'endpoints',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'UpdateEndpoint',
  },
  {
    name: '启用/禁用端点',
    category: 'endpoints',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'ToggleEndpoint',
  },
  {
    name: '测试端点',
    category: 'endpoints',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'TestEndpoint',
  },
  {
    name: '端点排序',
    category: 'endpoints',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'ReorderEndpoints',
    notes: 'API 已实现，UI 拖拽排序待完善',
  },
  {
    name: '获取模型列表',
    category: 'endpoints',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'FetchModels',
  },
  {
    name: '批量测试端点',
    category: 'endpoints',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'TestAllEndpointsZeroCost',
  },

  // ==================== 统计 ====================
  {
    name: '今日统计',
    category: 'stats',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'GetStatsDaily',
  },
  {
    name: '本周统计',
    category: 'stats',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'GetStatsWeekly',
  },
  {
    name: '本月统计',
    category: 'stats',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'GetStatsMonthly',
  },
  {
    name: '统计趋势图',
    category: 'stats',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'GetStatsTrend',
  },

  // ==================== 设置 ====================
  {
    name: '获取配置',
    category: 'settings',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'GetConfig',
  },
  {
    name: '修改端口',
    category: 'settings',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'UpdatePort',
  },
  {
    name: '主题切换',
    category: 'settings',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'SetTheme',
  },
  {
    name: '语言切换',
    category: 'settings',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'SetLanguage',
  },
  {
    name: '自动主题',
    category: 'settings',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'SetThemeAuto',
  },

  // ==================== 日志 ====================
  {
    name: '查看日志',
    category: 'logs',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'GetLogs',
  },
  {
    name: '按级别筛选',
    category: 'logs',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'GetLogsByLevel',
  },
  {
    name: '清空日志',
    category: 'logs',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'ClearLogs',
  },
  {
    name: '设置日志级别',
    category: 'logs',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'SetLogLevel',
  },

  // ==================== 终端 ====================
  {
    name: '打开终端',
    category: 'terminal',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'OpenTerminal',
  },
  {
    name: '会话列表',
    category: 'terminal',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'GetSessions',
  },
  {
    name: '打开会话终端',
    category: 'terminal',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'LaunchSessionTerminal',
  },
  {
    name: '删除会话',
    category: 'terminal',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'DeleteSession',
  },
  {
    name: '重命名会话',
    category: 'terminal',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'RenameSession',
  },

  // ==================== WebDAV ====================
  {
    name: 'WebDAV 配置',
    category: 'webdav',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'UpdateWebDAVConfig',
  },
  {
    name: 'WebDAV 连接测试',
    category: 'webdav',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'TestWebDAVConnection',
  },
  {
    name: 'WebDAV 备份',
    category: 'webdav',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'BackupToWebDAV',
  },
  {
    name: 'WebDAV 恢复',
    category: 'webdav',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'RestoreFromWebDAV',
  },

  // ==================== 其他 ====================
  {
    name: '版本信息',
    category: 'other',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'GetVersion',
  },
  {
    name: '更新日志',
    category: 'other',
    v1Status: 'implemented',
    v2Status: 'implemented',
    apiMethod: 'GetChangelog',
  },
  {
    name: '自动更新',
    category: 'other',
    v1Status: 'implemented',
    v2Status: 'not-implemented',
    notes: '待实现',
  },
  {
    name: '节日效果',
    category: 'other',
    v1Status: 'implemented',
    v2Status: 'not-implemented',
    notes: '待实现',
  },
  {
    name: '公告横幅',
    category: 'other',
    v1Status: 'implemented',
    v2Status: 'not-implemented',
    apiMethod: 'FetchBroadcast',
    notes: '待实现',
  },
];

// 统计函数
export function getFeatureStats() {
  const total = featureChecklist.length;
  const implemented = featureChecklist.filter(f => f.v2Status === 'implemented').length;
  const partial = featureChecklist.filter(f => f.v2Status === 'partial').length;
  const notImplemented = featureChecklist.filter(f => f.v2Status === 'not-implemented').length;

  return {
    total,
    implemented,
    partial,
    notImplemented,
    coverage: Math.round((implemented + partial * 0.5) / total * 100),
  };
}

// 按类别分组
export function getFeaturesByCategory() {
  const categories: Record<string, FeatureItem[]> = {};
  for (const feature of featureChecklist) {
    if (!categories[feature.category]) {
      categories[feature.category] = [];
    }
    categories[feature.category].push(feature);
  }
  return categories;
}

// 获取缺失功能
export function getMissingFeatures() {
  return featureChecklist.filter(f => f.v2Status !== 'implemented');
}
