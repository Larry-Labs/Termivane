# 选择性移植记录

基线：Larry-Labs/sshwifty `bd53b890ec81fadc80e75e73cec12d474839fe65`。
参考：nirui/sshwifty `0da08fc8f59ae05f9a1033fdcfc1b9b02fbb1af9`。
不合并上游分支，不替换现有产品、SFTP、会话恢复或部署配置。

## 执行状态

- [x] OSC 52 复制授权、流式字符编解码（保留界面定制）
- [x] 256 色终端、keyboard-interactive 认证（保留既有密码/私钥认证）
- [x] 会话输出背压、原子恢复、附件生命周期与 EOF 回归
- [x] SFTP 主机密钥绑定、交互认证的 SFTP 兼容
- [x] 可获取的依赖安全更新与构建验证
- [x] 自动化回归、浏览器验证及已知限制记录

密码记忆维持现有显式选择保存的行为。浏览器同源脚本可读取本地保存数据的问题不能靠把 XOR 改成同处保存密钥的加密解决；本次不将其宣称为安全密码库，不静默改变用户的持久化偏好。

## 实现与兼容边界

- 参考上游的功能设计，保留本分支 SSH command ID、session ID marker 和旧密码/私钥请求格式。新增 Keyboard Interactive 是显式可选认证方式，不照搬上游组合认证协议。
- 交互认证的答案只用于当前认证轮次，不进入浏览器密码记忆或服务端重连凭证；刷新优先恢复原连接。需要新建连接时重新回答挑战。
- SFTP 普通密码/私钥模式保持独立 SSH 连接，并校验终端已确认的主机密钥；交互认证使用原 SSH transport 的独立 SFTP channel，避免重放一次性验证码。此模式文件流量与终端共享网络连接。
- 实时输出使用有限队列和背压。附件切换与历史快照原子化，旧附件只能清理自身。两个输出流结束后再关闭 SSH。
- 保留 64 KiB 历史尾部与现有 12 小时 TTL；没有增加跨服务重启的会话保存能力。
- 保留 Vue 2、现有配色、字体设置、移动端、SFTP Worker、上传限速和部署方案。只修正阻止原有 lint/build 的格式、测试环境声明及锁文件问题。
- 测试脚本移除 Babel register，使用项目已声明的原生 ESM，避免 import.meta/SFTP Worker 模块被错误地当作 CommonJS；没有删测试或关闭断言。

## 验证记录（2026-11-08）

- `npm run lint`：0 errors，70 warnings（未通过关闭规则消除原有警告）。
- `npm run testonly`：前端 45 passing；Go 全库 `-race` 测试通过。
- `CGO_ENABLED=0 NODE_ENV=production ./node_modules/.bin/webpack --mode=production --config=webpack.config.js`：成功，3 项资源体积 warnings；静态 Go 资源生成成功。
- `CGO_ENABLED=0 go build -o .tmp/sshwifty-local .`：成功。
- 会话回归验证满队列不丢输出、Detach/Close 解阻塞、旧附件不影响新附件、历史/实时输出衔接、双流 EOF、并发切换。
- SFTP 真实 loopback SSH 测试验证错主机密钥在密码认证前被拒绝、正确密钥可列目录、关闭共享 SFTP 不关闭终端。临时恢复旧的忽略密钥行为时测试失败。
- Chromium + 本地应用 + `tests/e2e/fixture`：两轮交互认证（验证码为 password 输入）、两种认证的 SFTP、上传/下载内容比对、改名/目录创建、中文逐字节输出、两个标签页刷新保持原 SSH 会话 ID、密码记忆自动新建连接均通过。
- 真实终端 `burst`：10,000 行中文输出，逐行验证数量、顺序、内容和结束标记。
- OSC 52 实际页面显示授权条；独立真实组件浏览器测试覆盖拒绝、单次允许、会话允许和撤销。剪贴板写入适配器替换为内存收集器，没有读取或改写用户系统剪贴板。
- 390×844 页面 `scrollWidth == innerWidth == 390`，截图检查无页面横向溢出。

## 依赖与剩余限制

- Go 1.27.3、x/crypto 0.58.0、x/net 0.61.0 已实际下载并构建验证；Dockerfile 同步 Go 工具链要求，但本轮未执行 Docker 镜像构建或线上部署。
- npm audit 从 53 项降至 21 项：0 critical / 6 high / 12 moderate / 3 low。剩余主要涉及 Vue 2 构建链、favicons/sharp、glob、serialize-javascript；审计计数不等同于线上可利用性结论。本次没有引入 Vue 3 大迁移或强制覆盖不兼容依赖。
- 浏览器保存密码仍是用户主动选择的本地混淆存储，不能抵御同源恶意脚本；没有把它包装成安全密码库。
- 测试使用公开 fixture 数据和纯内存文件，未连接真实生产 SSH 目标。本地验证未连接生产环境，未推送或部署。产品展示名已按用户确认改为 Termivane，协议路径、配置键及构建产物名称保留兼容。
