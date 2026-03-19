Cursor + Cloudflare Tunnel 完整从 0 教程（含域名托管）

本教程适用于：
	•	让 Cursor 访问本地 OpenAI-compatible API（one-api / openai-forward / Ollama 等）
	•	解决 Cursor 无法访问 127.0.0.1（SSRF 限制）
	•	不需要服务器，永久免费方案

⸻

一、最终效果

Cursor
→ https://ai.example.com/v1
→ Cloudflare 边缘节点
→ Cloudflare Tunnel
→ 本地服务 http://127.0.0.1:2345

⸻

二、你需要准备的东西
	1.	一个域名（任意后缀，如 .com / .net / .dev）
	2.	一个 Cloudflare 账号（免费）
	3.	本地已经运行的 OpenAI-compatible API

示例本地 API 地址：

http://127.0.0.1:2345/v1

⸻

三、购买域名

你可以在以下任意平台注册购买域名：
	•	Namecheap
	•	Porkbun
	•	阿里云
	•	腾讯云
	•	Google Domains（已并入 Squarespace）

示例域名：

example.com

⸻

四、将域名托管到 Cloudflare（关键步骤）

4.1 注册并登录 Cloudflare

访问：

https://dash.cloudflare.com/

⸻

4.2 添加域名
	1.	点击 Add a site
	2.	输入你的域名（如 example.com）
	3.	选择 Free Plan

⸻

4.3 修改域名 NS（非常重要）

Cloudflare 会分配两条 Nameserver，例如：

alice.ns.cloudflare.com
bob.ns.cloudflare.com

前往你的域名购买平台后台：
	1.	找到 DNS / Nameserver 设置
	2.	删除原有 NS
	3.	替换为 Cloudflare 提供的两条 NS
	4.	保存设置

等待生效（通常 1–10 分钟，最长 24 小时）。

⸻

4.4 确认托管成功

在 Cloudflare 控制台中看到：

Status: Active

说明域名已成功托管到 Cloudflare。

⸻

五、本地安装 cloudflared

macOS

brew install cloudflare/cloudflare/cloudflared

Linux（示例）

curl -L https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64 -o cloudflared
chmod +x cloudflared
sudo mv cloudflared /usr/local/bin

验证安装

cloudflared version

⸻

六、登录 Cloudflare（一次性）

cloudflared login

浏览器会自动打开 Cloudflare 页面：
	1.	登录账号
	2.	选择你的域名
	3.	点击 Authorize

完成后会在本地生成认证文件。

⸻

七、创建 Tunnel

cloudflared tunnel create cursor-ai

记录以下信息：
	•	Tunnel 名称：cursor-ai
	•	credentials.json 文件路径

⸻

八、配置 Tunnel

8.1 创建配置目录

mkdir -p ~/.cloudflared

⸻

8.2 创建配置文件

nano ~/.cloudflared/config.yml

⸻

8.3 配置内容示例

tunnel: cursor-ai
credentials-file: /Users/你的用户名/.cloudflared/xxxx.json

ingress:
	•	hostname: ai.example.com
service: http://127.0.0.1:2345
	•	service: http_status:404

说明：
	•	ai.example.com：给 Cursor 使用的子域名
	•	service：本地 API 监听地址（不要加 /v1）

⸻

九、绑定子域名到 Tunnel（DNS）

cloudflared tunnel route dns cursor-ai ai.example.com

Cloudflare 会自动创建 DNS 记录，无需手动配置。

⸻

十、启动 Tunnel

cloudflared tunnel run cursor-ai

当看到：

Connected to Cloudflare

说明 Tunnel 已成功建立。

注意：该进程需要保持运行。

⸻

十一、验证 Tunnel 是否可用

curl https://ai.example.com/v1/models
	•	返回 JSON：成功
	•	返回 404 / 502：检查本地服务和端口

⸻

十二、Cursor 配置方式

打开 Cursor → Settings → Models，填写：

OpenAI API Key：任意值（如 sk-test）
Override OpenAI Base URL：开启
Base URL：https://ai.example.com/v1

注意事项：
	•	不要配置 Azure OpenAI
	•	不要使用 localhost / 127.0.0.1
	•	不要使用私有 IP

⸻

十三、常见问题排查

Cursor 报 SSRF / private IP
	•	Base URL 必须是公网域名
	•	检查是否误填本地地址

403 Forbidden
	•	Cloudflare 中关闭 WAF / Bot Fight
	•	确认 Tunnel 正常运行

模型不可用
	•	模型名称需与代理服务配置一致
	•	Cursor 使用 OpenAI-compatible 协议