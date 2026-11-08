# Termivane 部署指南

本指南针对本仓库的源码和 Dockerfile。这里的 `sshwifty:dev` 是自行构建的本地镜像名，不是已发布的镜像。构建时间取决于机器、网络和缓存。

## 本地构建，服务器运行

### 1. 构建最新源码

需要 Git、Node.js/npm 和 Go。Go 版本以 [`go.mod`](../go.mod) 为准；当前要求 **Go 1.27.3**。前端依赖使用仓库锁文件安装：

```bash
git clone https://github.com/Larry-Labs/Termivane.git
cd Termivane
npm ci
npm run build

# 交叉编译 Linux x86-64 服务器使用的二进制
./build-deploy.sh linux amd64
```

`npm run build` 会重新生成前端及嵌入 Go 的静态资源，并编译当前平台的 `sshwifty`。如只需刷新静态资源，可改用 `npm run generate`，再执行交叉编译脚本。

**更新源码后，应先执行 `npm run build` 或 `npm run generate`。** [`build-deploy.sh`](../build-deploy.sh) 只检查 `.tmp/dist` 和生成的 Go 文件是否存在，不检测源码是否变化；直接重复运行脚本可能沿用旧前端。不要把仓库里已有的二进制当作当前源码的构建结果。

ARM64 可生成对应二进制：

```bash
./build-deploy.sh linux arm64
```

但 [`Dockerfile.deploy`](../Dockerfile.deploy) 目前固定使用 `COPY sshwifty-linux-amd64 /sshwifty`。用于 ARM64 服务器时，须将这行的源文件改为 `sshwifty-linux-arm64`，传输对应二进制，并在 ARM64 环境构建镜像；脚本不会自动改写 Dockerfile。

### 2. 准备部署文件

以下示例以 Linux x86-64 为例，目标目录需预先创建：

```bash
scp sshwifty-linux-amd64 Dockerfile.deploy docker-compose.yml user@server:/opt/sshwifty/
```

启动前，在服务器上检查并调整 Compose 配置中的访问密码、端口和绑定地址。仓库内的 [`docker-compose.yml`](../docker-compose.yml) 发布 `8182` 到所有宿主机网卡；若反向代理运行在同一宿主机，可采用下面的 loopback 端口绑定示例。

### 3. 配置并启动

下面是需要自行填写环境变量的 Compose 示例。先在部署环境设置 `SSHWIFTY_SHAREDKEY`；不要提交真实密码到仓库。

```yaml
services:
  sshwifty:
    image: sshwifty:dev
    build:
      context: .
      dockerfile: Dockerfile.deploy
    restart: unless-stopped
    environment:
      SSHWIFTY_SHAREDKEY: "${SSHWIFTY_SHAREDKEY:?Set SSHWIFTY_SHAREDKEY before starting}"
      SSHWIFTY_LISTENINTERFACE: "0.0.0.0"
      SSHWIFTY_LISTENPORT: "8182"
      SSHWIFTY_READTIMEOUT: "43200"
      SSHWIFTY_WRITETIMEOUT: "43200"
      SSHWIFTY_HEARTBEATTIMEOUT: "60"
    ports:
      - "127.0.0.1:8182:8182/tcp"
```

容器内仍监听 `0.0.0.0`，通过 Docker 的宿主机端口绑定限制访问范围。反向代理若运行在另一容器，应使用共享 Docker 网络和服务名连接，而不是将其容器内的 `localhost` 当作宿主机。

```bash
cd /opt/sshwifty
docker compose up -d --build
```

更新容器会中断当前连接，服务端内存中的会话不能跨进程重启恢复。

### 4. 验证访问

本机可使用 `http://localhost:8182`；从其他机器通过域名或局域网 IP 访问时，应配置 **HTTPS**。认证使用 Web Crypto API，普通远程 HTTP 地址不满足浏览器的安全上下文要求。

打开页面后验证 Web 认证、SSH 登录、刷新恢复和 SFTP 文件操作。SSH 交互式认证不会保存或重放一次性验证码；如果原 SSH 连接已结束，需要重新完成挑战。

## 配置参数

| 参数 | 含义 |
|---|---|
| `SSHWIFTY_SHAREDKEY` | Web 访问密码；空值会关闭这一层访问认证 |
| `SSHWIFTY_READTIMEOUT` | 服务端连接的读超时限制，单位秒；不是会话保留时长 |
| `SSHWIFTY_WRITETIMEOUT` | 服务端连接的写超时限制，单位秒 |
| `SSHWIFTY_HEARTBEATTIMEOUT` | 参与 SSH WebSocket 心跳间隔计算，单位秒 |
| `SSHWIFTY_DIALTIMEOUT` | 远程连接建立超时，单位秒 |
| `SSHWIFTY_SOCKS5` | SSH 主连接可选的 SOCKS5 代理地址；独立 SFTP 连接当前直接拨号 |

示例里的读写超时为 43200 秒，不意味着 WebSocket 会无条件保持 12 小时。SSH WebSocket 另有 Ping/Pong 和数据接收截止时间；心跳间隔经配置归一化后，超出 60 秒的值会在处理器内回退为 30 秒。

持久化会话当前使用代码常量 `persistentSessionTTL`：**从会话创建起 12 小时到期**，后台每 5 分钟清理；重新附着不会续期。它与上述环境变量、浏览器访问密码的 12 小时记忆机制分别管理。参见[架构说明](architecture.md)。

### 使用配置文件

预设连接等选项见 [`sshwifty.conf.example.json`](../sshwifty.conf.example.json)。填写自己的配置，并确保容器内的非 root 用户有读取权限：

```bash
docker run -d \
  --restart unless-stopped \
  -p 127.0.0.1:8182:8182 \
  -v /path/to/sshwifty.conf.json:/etc/sshwifty.conf.json:ro \
  -e SSHWIFTY_CONFIG=/etc/sshwifty.conf.json \
  sshwifty:dev
```

使用配置文件时，在文件中设置 `SharedKey`、容器内监听地址和端口。

## HTTPS / TLS

### 反向代理

反向代理提供 HTTPS，Termivane 在宿主机 loopback 地址提供 HTTP。

Caddy 示例（域名解析和证书签发条件需满足 Caddy 的要求）：

```caddy
ssh.example.com {
    reverse_proxy 127.0.0.1:8182
}
```

Nginx 示例：

```nginx
server {
    listen 443 ssl;
    server_name ssh.example.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://127.0.0.1:8182;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
```

代理超时应覆盖正常心跳间隔，不必与会话 TTL 相同。上面的 Nginx 数值是示例，不是最长会话时长。修改代理配置后先执行对应配置检查，再重载。

### Termivane 直接提供 TLS

也可以挂载证书文件，避免将私钥内容放进命令行。先设置部署环境中的 `SSHWIFTY_SHAREDKEY`，并让容器内 `sshwifty` 用户能够读取挂载文件：

```bash
docker run -d \
  -p 8182:8182 \
  -e SSHWIFTY_SHAREDKEY \
  -v /path/to/cert.pem:/tls/cert.pem:ro \
  -v /path/to/key.pem:/tls/key.pem:ro \
  -e SSHWIFTY_TLSCERTIFICATEFILE=/tls/cert.pem \
  -e SSHWIFTY_TLSCERTIFICATEKEYFILE=/tls/key.pem \
  sshwifty:dev
```

浏览器使用与证书匹配的域名访问 `https://ssh.example.com:8182`。

## 在服务器从源码构建

若不使用交叉编译产物，可将完整源码放在构建环境，并把 Compose 的 `build.dockerfile` 改为 [`Dockerfile`](../Dockerfile)：

```bash
docker compose up -d --build
```

该路径会安装构建工具并编译前后端，需要访问相应软件源；构建耗时及不同架构的兼容性应在目标环境验证。

## 连接问题排查

项目实现了 TCP KeepAlive、WebSocket Ping/Pong 和 SSH keepalive，以帮助检测断链、减少空闲断连；它们不能保证网络中断或远端退出后仍保持连接。

1. 检查代理是否支持 WebSocket 升级，以及空闲超时是否短于心跳间隔。
2. 区分浏览器连接断开、远端 SSH 关闭、服务重启和会话到期。只有服务端原会话仍有效时，才能恢复同一个 SSH 会话。
3. 检查防火墙、NAT 和负载均衡的空闲连接策略。
4. 区分 SSH 与 SFTP 的路径：密码/私钥登录使用独立 SFTP SSH 连接；交互式认证复用已认证 transport 上的独立 SFTP 通道。

不要依赖 SSH WebSocket 内部的 AES-GCM 认证协议代替 HTTPS。保存到浏览器的密码使用可逆编码/混淆，不是独立密钥保护的密码库；只应在可信浏览器环境使用。
