# Augment Integration 实现文档

## 概述

ccNexus 现已集成 Augment 插件支持，在独立端口（默认 2346）提供 Augment 格式转换服务。

## 核心特性

1. **独立端口模式**：Augment 服务运行在独立端口，不干扰主 API
2. **零配置私钥**：私钥内置于二进制，首次启动自动初始化
3. **加密/明文双支持**：自动检测并处理加密或明文请求
4. **多格式转换**：支持转换为 Claude、OpenAI、CLI、Gemini 格式
5. **UI 可配置**：通过 UI 配置端口和启用/禁用服务

## 架构设计

### 目录结构

```
internal/augment/
├── decrypt/
│   └── decryptor.go          # RSA+AES 解密器
├── server/
│   └── server.go              # 独立 HTTP 服务器
internal/transformer/augment/  # 格式转换器
├── to_claude.go
├── to_openai.go
├── to_cli.go
└── to_gemini.go
```

### 请求流程

```
Augment Plugin
    ↓
    ↓ (加密/明文请求)
    ↓
:2346 Augment Server
    ↓
    ├─→ 检测 encrypted_data 字段
    ├─→ 如有加密则 RSA+AES 解密
    ├─→ 解析 Augment 格式
    ├─→ 转换为目标格式 (Claude/OpenAI/CLI/Gemini)
    ↓
上游 API (Claude/OpenAI/etc)
    ↓
    ↓ (响应)
    ↓
Augment Plugin
```

## 实现细节

### 1. 私钥管理

**嵌入方式**：
```go
//go:embed augment-private-key.pem
var augmentPrivateKeyPEM []byte
```

**自动初始化**：
- 启动时检查 `~/.ccNexus/augment-private-key.pem`
- 不存在则自动写入嵌入的私钥
- 权限设置为 0600

### 2. 配置扩展

**Config 结构新增字段**：
```go
type Config struct {
    // ...
    AugmentEnabled bool   `json:"augmentEnabled"`
    AugmentPort    int    `json:"augmentPort"`
    AugmentKeyPath string `json:"augmentKeyPath,omitempty"`
}
```

**默认值**：
- Enabled: false
- Port: 2346
- KeyPath: `~/.ccNexus/augment-private-key.pem`

### 3. 解密实现

**加密方案**：
- RSA-OAEP (SHA-256) 解密 AES 密钥
- AES-256-CBC 解密实际数据
- PKCS7 填充

**关键代码**：
```go
// 1. RSA 解密 AES 密钥
aesKey := rsa.DecryptOAEP(sha256, rand, privateKey, encryptedKey, nil)

// 2. AES 解密数据
block := aes.NewCipher(aesKey)
cbc := cipher.NewCBCDecrypter(block, iv)
cbc.CryptBlocks(plaintext, ciphertext)

// 3. 去除 PKCS7 填充
plaintext = removePKCS7Padding(plaintext)
```

### 4. 格式转换

**Augment → Claude**：
```go
// 转换请求
body, err := toClaudeRequest(augmentReq)
```

**Augment → OpenAI**：
```go
// 转换请求
body, err := toOpenAIRequest(augmentReq)
```

### 5. HTTP 服务器

**端点**：
- `POST /v1/messages` - Claude 格式
- `POST /v1/chat/completions` - OpenAI 格式
- `GET /health` - 健康检查

**请求处理**：
1. 读取请求体
2. 检测 `encrypted_data` 字段
3. 如有加密则解密
4. 根据路径选择转换器
5. 转换格式并代理到上游
6. 返回响应

### 6. 生命周期管理

**启动**：
```go
func (a *App) startup(ctx context.Context) {
    // ...
    a.initAugmentServer(configDir, cfg)
    // ...
}
```

**停止**：
```go
func (a *App) shutdown(ctx context.Context) {
    if a.augmentServer != nil {
        a.augmentServer.Stop()
    }
    // ...
}
```

### 7. Wails 绑定

**前端可用方法**：
```go
// 获取配置
func (a *App) GetAugmentConfig() string

// 保存配置
func (a *App) SaveAugmentConfig(settingsJSON string) error
```

**配置格式**：
```json
{
  "enabled": true,
  "port": 2346
}
```

## 使用指南

### 启用 Augment 服务

1. 打开 ccNexus UI
2. 进入设置页面
3. 找到 Augment 配置项
4. 启用服务并设置端口（默认 2346）
5. 保存并重启应用

### VSCode Augment 插件配置

在 VSCode 设置中配置：
```json
{
  "augment.apiEndpoint": "http://localhost:2346",
  "augment.apiKey": "your-api-key"
}
```

### 验证服务运行

```bash
# 检查端口监听
lsof -i :2346

# 测试健康检查
curl http://localhost:2346/health

# 测试请求
curl -X POST http://localhost:2346/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: your-key" \
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "messages": [{"role": "user", "content": "Hello"}],
    "max_tokens": 100
  }'
```

## 安全考虑

1. **私钥保护**：
   - 文件权限 0600（仅所有者可读写）
   - 存储在用户目录 `~/.ccNexus/`
   - 不随代码仓库分发

2. **本地监听**：
   - 默认仅监听 127.0.0.1
   - 不暴露到公网

3. **API Key 验证**：
   - 继承主配置的 API Key
   - 请求头验证

## 故障排查

### 服务未启动

**症状**：端口未监听，无法连接

**排查**：
1. 检查配置：`augmentEnabled: true`
2. 查看日志：`~/.ccNexus/debug.log`
3. 确认端口未被占用：`lsof -i :2346`
4. 验证私钥文件存在：`ls -la ~/.ccNexus/augment-private-key.pem`

### 解密失败

**症状**：加密请求返回 400 错误

**排查**：
1. 确认私钥文件完整且可读
2. 检查 Augment 插件使用的公钥是否匹配
3. 查看日志中的详细错误信息

### 上游 API 错误

**症状**：请求被接受但返回错误

**排查**：
1. 验证上游 API 配置正确
2. 检查 API Key 是否有效
3. 确认模型名称映射正确
4. 查看上游 API 返回的错误信息

## 开发说明

### 添加新的格式转换器

1. 在 `internal/transformer/augment/` 创建新文件
2. 实现转换函数：
```go
func ToNewFormat(augReq *augment.Request) (interface{}, error) {
    // 转换逻辑
}
```
3. 在 `server.go` 中添加路由处理

### 修改加密算法

如需更改加密方案，修改 `internal/augment/decrypt/decryptor.go`：
- 更新 RSA 参数
- 更改 AES 模式
- 调整填充方式

### 扩展配置选项

1. 在 `internal/config/config.go` 添加字段
2. 更新 `GetAugmentConfig()` 和 `UpdateAugmentConfig()`
3. 修改 Wails 绑定方法
4. 更新前端 UI

## 性能优化

1. **连接复用**：HTTP 客户端使用连接池
2. **超时控制**：请求超时 30 秒
3. **并发处理**：每个请求独立 goroutine
4. **内存管理**：及时释放大对象

## 未来改进

- [ ] 支持流式响应
- [ ] 添加请求缓存
- [ ] 实现速率限制
- [ ] 支持多私钥轮换
- [ ] 添加详细的请求日志
- [ ] 支持自定义转换规则

## 参考资料

- [Augment VSCode 插件](https://marketplace.visualstudio.com/items?itemName=augment.augment)
- [augment-proxy 原项目](https://github.com/your-repo/augment-proxy)
- [Claude API 文档](https://docs.anthropic.com/claude/reference)
- [OpenAI API 文档](https://platform.openai.com/docs/api-reference)
