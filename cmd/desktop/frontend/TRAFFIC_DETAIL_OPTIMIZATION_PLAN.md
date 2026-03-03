# 请求详情 JSON 显示优化方案

> 针对「原始请求 / 转换后请求 / 原始响应 / 转换后响应」四个 Tab 的展示优化

---

## 一、当前问题

| 问题 | 说明 |
|------|------|
| 折叠预览不直观 | `9 keys`、`1 items`、`{ 2 keys }` 无法快速了解内容 |
| 缩进线突兀 | 左侧绿色竖线在浅色背景下过于抢眼 |
| 配色不统一 | 与主题变量脱节，暗色模式体验一般 |
| 无快捷操作 | 缺少复制、全部展开/折叠 |
| Tab 样式单调 | 纯文字，无图标，区分度低 |

---

## 二、优化方案（按优先级）

### 方案 A：改进树形视图（推荐，改动小）

**目标**：在现有树形结构上提升可读性和观感。

| 改动项 | 具体措施 |
|--------|----------|
| 1. 折叠预览 | 用首项预览替代「X keys」：如 `{model, messages, system, ...}` |
| 2. 缩进样式 | 去掉 `border-left`，改用 `padding-left` + 背景色块区分层级 |
| 3. 配色 | 使用主题变量，支持亮/暗模式 |
| 4. 字体 | 统一为 `ui-monospace` 或 `JetBrains Mono` |
| 5. 操作栏 | 在 Tab 下方增加「复制」「全部展开」「全部折叠」按钮 |

**预估**：约 80 行 JS + 60 行 CSS

---

### 方案 B：增加「源码视图」模式（推荐，体验好）

**目标**：提供树形 + 源码两种展示方式。

| 模式 | 说明 | 适用场景 |
|------|------|----------|
| 树形视图 | 可折叠、层级清晰 | 浏览结构、定位字段 |
| 源码视图 | `JSON.stringify(obj, null, 2)` 格式化 | 复制、对比、调试 |

**实现**：
- 每个 Tab 内容区右上角增加「树形 / 源码」切换
- 源码模式：`<pre>` + 简单语法高亮（正则替换 key/string/number）
- 复制按钮：一键复制当前 Tab 的原始 JSON 字符串

**预估**：约 120 行 JS + 40 行 CSS

---

### 方案 C：Tab 与布局优化

| 改动项 | 具体措施 |
|--------|----------|
| Tab 图标 | 为四个 Tab 增加小图标（请求/响应、原始/转换） |
| Tab 样式 | 与 workspace-tab 统一，圆角、hover 效果 |
| 内容区 | 增加浅色背景区分，适当内边距 |

---

### 方案 D：引入轻量 JSON 组件（可选）

若希望更专业的展示，可引入：

- **json-viewer**（~3KB）或类似库
- 或自研基于 `details/summary` 的折叠结构

**权衡**：功能更全，但增加依赖和打包体积。

---

## 三、推荐实施顺序

1. **Phase 1**：方案 A（树形改进）+ 方案 C（Tab 优化）
2. **Phase 2**：方案 B（源码视图 + 复制）

---

## 四、配色参考（主题变量）

```css
/* 亮色 */
.json-key { color: var(--primary-color); }
.json-string { color: var(--success-color); }
.json-number { color: #1750eb; }
.json-boolean { color: #1750eb; }
.json-null { color: var(--text-tertiary); }

/* 暗色由主题自动覆盖 */
```

---

## 五、关键代码位置

| 文件 | 职责 |
|------|------|
| `modules/traffic.js` | `formatJSON`、`renderJSONTree`、`renderTrafficDetailModal` |
| `style.css` | `.json-tree`、`.traffic-json`、`.traffic-detail-tabs` |
| `ui.js` | `#trafficDetailModal` 的 HTML 结构 |
