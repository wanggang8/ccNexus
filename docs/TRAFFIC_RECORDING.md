# 流量记录功能使用说明

## 功能概述

ccNexus Server 端已集成完整的流量记录功能，可以记录和查看所有通过代理的 API 请求和响应。

## API 端点

### 1. 获取录制状态
```bash
GET /api/traffic/recording
```

响应示例：
```json
{
  "success": true,
  "data": {
    "recording": true
  }
}
```

### 2. 开启/关闭录制
```bash
POST /api/traffic/recording
Content-Type: application/json

{
  "recording": true
}
```

### 3. 获取流量日志列表
```bash
GET /api/traffic/logs
```

响应示例：
```json
{
  "success": true,
  "data": {
    "logs": [
      {
        "id": "uuid",
        "timestamp": "2024-01-01T12:00:00Z",
        "endpoint_name": "openai",
        "client_format": "openai",
        "method": "POST",
        "path": "/v1/chat/completions",
        "status_code": 200,
        "duration_ms": 1234
      }
    ],
    "total": 1
  }
}
```

### 4. 获取日志详情
```bash
GET /api/traffic/logs/:id
```

响应包含完整的请求和响应内容。

### 5. 清空日志
```bash
POST /api/traffic/clear
```

## Web UI

访问 `http://localhost:8787/ui/`，在左侧导航中进入 Traffic 页面即可使用图形界面：

- 查看流量日志列表
- 查看请求/响应详情
- 开启/关闭录制
- 清空日志

## 测试

运行测试脚本：
```bash
./test_traffic.sh
```

或运行单元测试：
```bash
go test -v ./cmd/server/webui/api -run TestTrafficAPIs
```

## 注意事项

- 日志存储在内存中，最多保留 500 条记录（环形缓冲区）
- 服务重启后日志会清空
- 录制状态默认为关闭，需要手动开启
