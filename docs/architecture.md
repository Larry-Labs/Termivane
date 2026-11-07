# Termivane 架构设计

## 系统总览

Termivane 是一个基于 Web 的 SSH 客户端，采用 Go 后端 + Vue 2 前端的架构。浏览器通过 WebSocket 与服务器通信，服务器代理 SSH/SFTP 连接到远程主机。

```
┌─────────────────────────────────────────────────────────────────────┐
│                          Browser                                    │
│                                                                     │
│  ┌─────────────────┐    ┌──────────────┐    ┌────────────────────┐  │
│  │  xterm.js        │    │  Vue 2 UI    │    │  Web Worker        │  │
│  │  Terminal         │    │  (home.vue)  │    │  (sftp.worker.js)  │  │
│  │  Emulator         │    │              │    │                    │  │
│  └────────┬──────────┘    └──────┬───────┘    └─────────┬──────────┘  │
│           │                      │                      │            │
│           │         WS #1        │                      │  WS #2     │
│           │    (AES-GCM 加密)    │                      │ JSON/Binary│
└───────────┼──────────────────────┼──────────────────────┼────────────┘
            │                      │                      │
            ▼                      ▼                      ▼
┌───────────────────────────────────────────────────────────────────────┐
│                        Go Server (:8182)                              │
│                                                                       │
│  ┌──────────┐  ┌────────────┐  ┌──────────────┐  ┌────────────────┐  │
│  │ HTTP     │  │ WebSocket  │  │ SFTP         │  │ Persistent     │  │
│  │ Router   │──│ Handler    │  │ WebSocket    │  │ Session Mgr    │  │
│  │          │  │ (socket)   │  │ (sftpSocket) │  │ (Ring Buffer)  │  │
│  └──────────┘  └─────┬──────┘  └──────┬───────┘  └───────┬────────┘  │
│                      │               │                   │           │
│                      ▼               ▼                   │           │
│              ┌───────────────────────────────┐            │           │
│              │     SSH TCP Connection(s)     │◄───────────┘           │
│              │     (golang.org/x/crypto)     │                       │
│              └───────────────┬───────────────┘                       │
└──────────────────────────────┼───────────────────────────────────────┘
                               │
                               ▼
                    ┌──────────────────────┐
                    │   Remote SSH Server  │
                    │   (sshd :22)         │
                    └──────────────────────┘
```

## 后端架构 (Go)

### 模块结构

```
sshwifty.go                          # 主入口：加载配置 → 启动服务
│
├── application/
│   ├── application.go               # 应用生命周期管理、信号处理
│   ├── plate.go                     # 版本信息
│   │
│   ├── configuration/               # 配置加载（文件 / 环境变量 / 冗余策略）
│   │   ├── loader_file.go           #   JSON 文件加载器
│   │   ├── loader_enviro.go         #   环境变量加载器
│   │   ├── loader_redundant.go      #   冗余（多源优先级）加载器
│   │   ├── config.go                #   配置结构定义
│   │   ├── server.go                #   服务器级配置（端口/超时/TLS）
│   │   └── preset.go                #   预设连接配置
│   │
│   ├── controller/                  # HTTP 路由与请求处理
│   │   ├── controller.go            #   路由分发器（/ → home, /sshwifty/socket → WS）
│   │   ├── socket.go                #   SSH WebSocket 通道（AES-GCM 加密）
│   │   ├── socket_sftp.go           #   SFTP WebSocket（独立连接或复用 transport）
│   │   ├── socket_verify.go         #   WebSocket 认证验证
│   │   ├── reconnect.go             #   断线重连 token 验证
│   │   ├── reattach.go              #   持久化会话重新附着
│   │   ├── home.go                  #   首页静态页面
│   │   ├── static.go                #   静态资源服务
│   │   └── static_pages/            #   [生成] webpack 产物嵌入 Go 二进制
│   │
│   ├── commands/                    # SSH 命令实现
│   │   ├── ssh.go                   #   SSH 连接、认证、PTY、数据转发
│   │   ├── ssh_interactive.go        #   SSH 多轮问答式认证
│   │   ├── sessions.go              #   会话注册中心（Session/Reconnect Token）
│   │   ├── persistent_session.go    #   持久化会话（Ring Buffer + SessionAttachment）
│   │   └── commands.go              #   命令注册
│   │
│   ├── command/                     # 命令框架（协议层）
│   │   ├── handler.go               #   流处理器
│   │   ├── streams.go               #   多路复用流管理
│   │   ├── header.go                #   协议头解析
│   │   ├── hooks.go                 #   连接前钩子
│   │   └── fsm.go                   #   有限状态机
│   │
│   ├── network/                     # 网络拨号
│   │   ├── dial.go                  #   直连拨号
│   │   ├── dial_socks5.go           #   SOCKS5 代理拨号
│   │   └── conn_timeout.go          #   超时连接包装
│   │
│   ├── log/                         # 日志
│   ├── rw/                          # 读写工具（FetchReader 等）
│   └── server/                      # HTTP Server 启动
```

### 路由表

| 路径 | 处理器 | 说明 |
|---|---|---|
| `/` | `home` | 首页（SPA 入口） |
| `/sshwifty/socket` | `socket` | SSH 终端 WebSocket |
| `/sshwifty/socket/verify` | `socketVerification` | SharedKey 认证验证 |
| `/sshwifty/sftp` | `sftpSocket` | SFTP 文件操作 WebSocket |
| `/sshwifty/reconnect` | `reconnectController` | 断线重连 |
| `/sshwifty/session/reattach` | `reattachController` | 持久化会话重新附着 |
| `/sshwifty/assets/*` | 静态文件 | JS/CSS/图片等前端资源 |

## 前端架构 (Vue 2 + Webpack)

### 模块结构

```
ui/
├── app.js                           # 应用入口
├── app.css / common.css             # 全局样式
├── index.html                       # SPA 模板
│
├── home.vue                         # 主界面（标签页管理、会话恢复）
├── auth.vue                         # SharedKey 认证页
├── loading.vue                      # 加载中页面
│
├── commands/                        # SSH 命令处理
│   ├── ssh.js                       #   SSH 命令协议实现（认证、数据收发）
│   ├── commands.js                  #   命令注册与管理
│   ├── credentials.js               #   密码保存（localStorage XOR 编码）
│   ├── keyboard_interactive.js       #   交互认证问题解析与回答编码
│   ├── presets.js                   #   预设连接管理
│   └── common.js / events.js        #   公共工具
│
├── sftp/                            # SFTP 文件管理器
│   ├── client.js                    #   SFTPClient 类（主线程代理）
│   └── sftp.worker.js               #   Web Worker（WebSocket + 文件传输）
│
├── widgets/                         # UI 组件
│   ├── screen_console.vue           #   终端控制台（xterm.js + SFTP 面板）
│   ├── clipboard.js / clipboard.css  #   OSC 52 复制请求与授权界面
│   ├── connector.vue                #   连接对话框（SSH 参数输入）
│   ├── tab_list.vue / tabs.vue      #   标签页管理
│   ├── status.vue                   #   连接状态指示器
│   └── ...                          #   其他组件
│
├── iconv/                           # 终端流式字符编解码
│
├── stream/                          # 通信协议层
│   ├── stream.js                    #   WebSocket 流封装
│   ├── streams.js                   #   多路复用管理
│   ├── sender.js / reader.js        #   数据发送/接收
│   ├── header.js                    #   协议头
│   └── subscribe.js                 #   事件订阅
│
├── control/                         # 控制层
│   └── ssh.js                       #   SSH 控制逻辑
│
├── socket.js                        # WebSocket 连接管理
├── crypto.js                        # AES-GCM 加密（与服务端配对）
├── history.js                       # 连接历史
└── xhr.js                           # HTTP 请求工具
```

## 核心机制

### 1. 双 WebSocket 通道

SSH 终端和 SFTP 使用独立的 WebSocket。SFTP 的 SSH 传输方式取决于认证方法：

- **密码/私钥认证**：SFTP 创建独立 SSH TCP 连接，并校验主连接已经确认的服务器公钥。文件操作不会复用终端的 SSH channel。
- **交互式认证**：SFTP 在已认证的 SSH transport 上新建 channel，避免为一次性验证码发起第二次认证。关闭 SFTP channel 不关闭终端 transport。

```text
浏览器终端 ── WS #1 ── SSH handler ── SSH transport ── PTY channel
                                              └──── SFTP channel（交互认证）
浏览器 Worker ─ WS #2 ─ SFTP handler ────────────┘
                              └── 独立 SSH transport ─ SFTP（密码/私钥）
```

- **WS #1**：自定义二进制协议，通过 `command.Handler` 多路复用；内部 AES-GCM 用于访问认证，不代替 HTTPS。
- **WS #2**：JSON 控制消息与文件二进制数据，前端由 Web Worker 处理。使用 HTTPS 时，两条浏览器连接均通过 WSS 传输。
- 这些连接仍共享服务器、远端和网络资源，不承诺文件传输对终端完全没有影响。SSH 主连接支持配置的 SOCKS5 拨号路径；独立 SFTP 当前使用直接拨号。

### 2. 持久化会话（Persistent Session）

浏览器断开时，SSH 连接和最近输出保留在服务端内存；刷新页面可尝试重新附着原会话。远端退出、服务重启或会话过期后，不能恢复同一会话。

```text
SSH stdout / stderr
        │
        ▼
  PersistentSession.publish()
        ├── RingBuffer：保留最近 64 KiB 输出
        └── 当前 SessionAttachment：有界队列 → 浏览器

刷新恢复：
  Attach() 原子切换 attachment，并返回此前输出的 snapshot
        ├── 先回放 snapshot
        └── 再读取新 attachment 的实时输出

断开清理：
  Detach(attachment) 只移除调用方持有的 attachment
  旧页面的清理不会断开已接管会话的新页面
```

**关键边界：**

- `RingBuffer` 是有限的最近输出回放，不是完整会话录像；浏览器端的 999,999 行滚动缓冲区与它独立。
- 活跃 attachment 的队列满时施加背压，不静默丢弃终端输出；断开或替换 attachment 可解除等待。
- stdout/stderr 都排空后才关闭正常结束的 SSH 会话；读取错误会终止会话。
- 当前 `ExpiresAt` 在创建会话时设为 **12 小时后**。`Attach()` 不续期；过期 token 不再允许附着，`GlobalPersistentSessions` 每 5 分钟清理过期会话。
- 会话 TTL 不由 `SSHWIFTY_READTIMEOUT` 控制，也不是“每次断开后再延长 12 小时”。
- 浏览器的多标签恢复信息保存在 `sessionStorage`，不是跨设备同步。

### 3. SFTP 自适应限速（AIMD 算法）

上传文件时通过 SSH keepalive 请求耗时动态调整速率，以减轻带宽竞争；这一信号包含网络与远端处理耗时，不是精确的网络拥塞测量，也不能保证终端无延迟：

```
速率
 ▲
 │     ┌───┐
 │    ╱    │    ╱─── 线性增长 (latency < 100ms: +256 KiB/s)
10M──┤     │   ╱
 │   │     │  ╱
 │   │     ╰─╱──── 乘性衰减 (latency > 300ms: ×2/3)
 │   │       │
512K─┼───────┤
 │   │       │
 └───┴───────┴───────► 时间

探测机制：
  每 2 秒发送 keepalive@openssh.com 请求
  测量 keepalive 请求耗时 → 调整上传速率

参数：
  最小速率: 512 KiB/s
  最大速率: 10 MiB/s
  初始速率: 2 MiB/s
  低延迟阈值: 100ms (增速)
  高延迟阈值: 300ms (降速)
  控制粒度: 50ms tick, 每 tick 发送 rate/20 字节
```

### 4. 连接保活（多层机制）

从 OS 层到应用层提供多级保活，用于检测断链并减少空闲断连：

| 层级 | 机制 | 间隔 | 代码位置 |
|------|------|------|----------|
| OS/TCP | 接入连接 TCP KeepAlive | 30s | `application/server/conn.go` Accept() |
| WebSocket | Ping/Pong | HeartbeatTimeout | `application/controller/socket.go` |
| WebSocket | ReadDeadline 数据刷新 | 每次收到数据 | `application/controller/socket.go` buildWSFetcher() |
| SSH | SFTP SSH KeepAlive | 30s | `application/controller/socket_sftp.go` |
| SSH | 持久化会话 KeepAlive | 30s | `application/commands/persistent_session.go` |

HTTP Server 的 `ReadTimeout`/`WriteTimeout` 设为 0（禁用），避免在 WebSocket 升级后仍持有底层连接的绝对 deadline。`ReadHeaderTimeout` 保留以限制请求头读取时间。底层连接包装仍按配置限制读写 deadline。

- SSH WebSocket：以归一化后的 `HeartbeatTimeout` 计算 Ping 间隔；处理器将 ≤ 0 或 > 60 秒的值回退为 30 秒。接收截止时间为 Ping 间隔 + 10 秒，收到 Pong 或数据时刷新。
- SFTP WebSocket：双向保活，服务端 Ping (30s) + 客户端 Worker JSON ping (30s)

### 5. 认证流程

```
Browser                              Server
   │                                    │
   │──── WS Connect ───────────────────►│
   │                                    │
   │──── Client Nonce ─────────────────►│
   │◄─── Server Nonce ─────────────────│
   │                                    │
   │     双方基于 SharedKey + UserAgent  │
   │     + HostName / 时间戳派生密钥     │
   │     此 WS 后续帧使用 AES-GCM         │
   │                                    │
   │──── SSH Connect Request ──────────►│
   │     (host, user, auth method)      │──── SSH Dial ────►
   │                                    │                    Remote
   │◄─── Fingerprint Verify ───────────│◄─── Host Key ─────
   │──── Accept/Reject ───────────────►│
   │                                    │
   │◄─── Credential Request ──────────│
   │──── Password/Key/Answers ────────►│──── SSH Auth ─────►
   │                                    │
   │◄─── Connect Succeed ─────────────│◄─── Session Open ──
   │     + SessionID + ReconnectToken   │
   │                                    │
   │◄════ Terminal I/O ═══════════════►│◄════ PTY I/O ════►
```

SSH 登录支持密码、私钥和 keyboard-interactive。交互认证可包含多轮问题，并按服务器提供的 echo 标志显示或隐藏输入；回答不存入密码记忆或重连凭据。它与 Web 层的 SharedKey 认证是两层不同的认证。

Web 访问密码默认记忆 12 小时，使用 Base64 存入 `localStorage`；用户选择保存的 SSH 密码采用同源存储中的设备值进行 XOR 混淆。这些都是可逆存储，不能提供抵抗同源脚本读取的密码库安全性。页面应部署在 HTTPS 安全上下文中；localhost 本地开发是例外。

### 6. 终端能力

- PTY 请求使用 `xterm-256color`，向远端声明 256 色能力。
- `ui/iconv/` 保留跨数据块的字符解码状态，避免把一个多字节字符的两部分分别解码。
- OSC 52 远端复制请求由剪贴板组件处理，用户可允许一次或当前会话内允许；默认不自动写入系统剪贴板，也不读取系统剪贴板内容。浏览器仍可限制剪贴板写入。

## 构建流程

```
源码                      构建过程                           产物
──────                    ──────────                         ──────
ui/**                     npm run generate
├── *.vue                 ├── webpack --mode production       .tmp/dist/
├── *.js                  │   ├── babel 转译                  ├── app-*.js
├── *.css                 │   ├── vue-loader                  ├── app-*.css
└── sshwifty.svg          │   ├── favicon 生成                ├── *.png/ico
                          │   ├── CSS/JS 压缩                 └── index.html
                          │   └── 图片优化 (sharp)
                          │
                          └── go generate ./...
                              └── 将 .tmp/dist/ 文件           static_pages/
                                  编码为 Go 字节数组            └── static*_generated.go

                          go build
                          ├── 嵌入静态资源                     sshwifty
                          ├── 编译所有 Go 模块                 (单一二进制)
                          └── -ldflags 注入版本号
```

`npm run build` 包含前端生成与 Go 编译。交叉编译脚本仅检查已有产物，不检测源码变化；更新后必须先运行 `npm run build` 或 `npm run generate`，再执行 `build-deploy.sh`。部署架构限制见[部署指南](deployment.md)。

## 技术栈

以下版本对应当前 [`go.mod`](../go.mod) 与 [`package-lock.json`](../package-lock.json)。

| 层级 | 技术 | 版本 |
|---|---|---|
| **后端语言** | Go | 1.27.3 |
| **SSH 库** | golang.org/x/crypto | v0.58.0 |
| **SFTP 库** | github.com/pkg/sftp | v1.13.10 |
| **WebSocket** | github.com/gorilla/websocket | v1.5.3 |
| **前端框架** | Vue.js | 锁定 2.7.16，保持 Vue 2 |
| **终端模拟** | xterm.js | 6.0 |
| **构建工具** | Webpack | 5.x |
| **容器化** | Docker (Alpine) | - |
