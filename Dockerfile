# Build the build base environment
FROM ubuntu:24.04 AS base
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}" \
    GOPATH="/root/go" \
    GOTOOLCHAIN="local"
RUN set -ex && \
    cd / && \
    echo '#!/bin/sh' > /try.sh && echo 'res=1; for i in $(seq 0 36); do $@; res=$?; [ $res -eq 0 ] && exit $res || sleep 10; done; exit $res' >> /try.sh && chmod +x /try.sh && \
    echo '#!/bin/sh' > /child.sh && echo 'cpid=""; ret=0; i=0; for c in "$@"; do ( (((((eval "$c"; echo $? >&3) | sed "s/^/|-($i) /" >&4) 2>&1 | sed "s/^/|-($i)!/" >&2) 3>&1) | (read xs; exit $xs)) 4>&1) & ppid=$!; cpid="$cpid $ppid"; echo "+ Child $i (PID $ppid): $c ..."; i=$((i+1)); done; for c in $cpid; do wait $c; cret=$?; [ $cret -eq 0 ] && continue; echo "* Child PID $c has failed." >&2; ret=$cret; done; exit $ret' >> /child.sh && chmod +x /child.sh && \
    export PATH=$PATH:/ && \
    export DEBIAN_FRONTEND=noninteractive && \
    (rm -f /etc/apt/sources.list.d/ubuntu.sources 2>/dev/null; echo "deb http://mirrors.tuna.tsinghua.edu.cn/ubuntu/ noble main restricted universe multiverse" > /etc/apt/sources.list && echo "deb http://mirrors.tuna.tsinghua.edu.cn/ubuntu/ noble-updates main restricted universe multiverse" >> /etc/apt/sources.list && echo "deb http://mirrors.tuna.tsinghua.edu.cn/ubuntu/ noble-security main restricted universe multiverse" >> /etc/apt/sources.list) && \
    ([ -z "$HTTP_PROXY" ] || (echo "Acquire::http::Proxy \"$HTTP_PROXY\";" >> /etc/apt/apt.conf)) && \
    ([ -z "$HTTPS_PROXY" ] || (echo "Acquire::https::Proxy \"$HTTPS_PROXY\";" >> /etc/apt/apt.conf)) && \
    (echo "Acquire::Retries \"8\";" >> /etc/apt/apt.conf) && \
    echo '#!/bin/sh' > /install.sh && echo 'apt-get -y update && apt-get -y --fix-broken install autoconf automake libtool build-essential ca-certificates curl git nodejs npm libvips libvips-dev libpng-dev' >> /install.sh && chmod +x /install.sh && \
    /try.sh /install.sh && rm /install.sh && \
    /try.sh update-ca-certificates -f && c_rehash && \
    curl -fsSL https://golang.google.cn/dl/go1.27.3.linux-$(dpkg --print-architecture).tar.gz | tar -C /usr/local -xz && \
    export PATH=/usr/local/go/bin:$PATH && \
    export GOPATH=/root/go && \
    npm config set registry https://registry.npmmirror.com && \
    ([ -z "$HTTP_PROXY" ] || (git config --global http.proxy "$HTTP_PROXY" && npm config set proxy "$HTTP_PROXY")) && \
    ([ -z "$HTTPS_PROXY" ] || (git config --global https.proxy "$HTTPS_PROXY" && npm config set https-proxy "$HTTPS_PROXY")) && \
    export GOPROXY=https://goproxy.cn,direct && \
    ([ -z "$CUSTOM_COMMAND" ] || (echo "Running custom command: $CUSTOM_COMMAND" && $CUSTOM_COMMAND)) && \
    export N_NODE_MIRROR=https://npmmirror.com/mirrors/node && \
    echo '#!/bin/sh' > /install.sh && echo "(N_NODE_MIRROR=https://npmmirror.com/mirrors/node npm install -g n && N_NODE_MIRROR=https://npmmirror.com/mirrors/node n stable) || (npm cache clean -f && false)" >> /install.sh && chmod +x /install.sh && /try.sh /install.sh && rm /install.sh && \
    git version && \
    go version && \
    npm version

# Install dependencies (cached unless package.json/go.mod change)
FROM base AS deps
RUN mkdir -p /tmp/.build/sshwifty
COPY package.json package-lock.json /tmp/.build/sshwifty/
COPY _packages/ /tmp/.build/sshwifty/_packages/
COPY go.mod go.sum /tmp/.build/sshwifty/
RUN set -ex && \
    cd / && \
    export PATH=$PATH:/ && \
    export DEBIAN_FRONTEND=noninteractive && \
    export CPPFLAGS='-DPNG_ARM_NEON_OPT=0' && \
    export GOPROXY=https://goproxy.cn,direct && \
    /child.sh \
        "cd /tmp/.build/sshwifty && echo '#!/bin/sh' > /npm_install.sh && echo \"npm install || (npm cache clean -f && rm ~/.npm/_* -rf && false)\" >> /npm_install.sh && chmod +x /npm_install.sh && /try.sh /npm_install.sh && rm /npm_install.sh" \
        'cd /tmp/.build/sshwifty && export GOPROXY=https://goproxy.cn,direct && /try.sh go mod download'

# Copy source and build (only re-runs when source files change)
FROM deps AS builder
COPY . /tmp/.build/sshwifty
RUN --mount=type=cache,target=/root/.cache/go-build,id=sshwifty-gobuild \
    set -ex && \
    cd / && \
    export PATH=$PATH:/ && \
    export GOPROXY=https://goproxy.cn,direct && \
    ([ -z "$HTTP_PROXY" ] || (git config --global http.proxy "$HTTP_PROXY" && npm config set proxy "$HTTP_PROXY")) && \
    ([ -z "$HTTPS_PROXY" ] || (git config --global https.proxy "$HTTPS_PROXY" && npm config set https-proxy "$HTTPS_PROXY")) && \
    (cd /tmp/.build/sshwifty && /try.sh npm run build && mv ./sshwifty /)

# Build the final image for running
FROM alpine:latest
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories
ENV SSHWIFTY_HOSTNAME= \
    SSHWIFTY_SHAREDKEY= \
    SSHWIFTY_DIALTIMEOUT=10 \
    SSHWIFTY_SOCKS5= \
    SSHWIFTY_SOCKS5_USER= \
    SSHWIFTY_SOCKS5_PASSWORD= \
    SSHWIFTY_HOOK_BEFORE_CONNECTING= \
    SSHWIFTY_HOOKTIMEOUT=30 \
    SSHWIFTY_LISTENINTERFACE=0.0.0.0 \
    SSHWIFTY_LISTENPORT=8182 \
    SSHWIFTY_INITIALTIMEOUT=0 \
    SSHWIFTY_READTIMEOUT=0 \
    SSHWIFTY_WRITETIMEOUT=0 \
    SSHWIFTY_HEARTBEATTIMEOUT=0 \
    SSHWIFTY_READDELAY=0 \
    SSHWIFTY_WRITEELAY=0 \
    SSHWIFTY_TLSCERTIFICATEFILE= \
    SSHWIFTY_TLSCERTIFICATEKEYFILE= \
    SSHWIFTY_DOCKER_TLSCERT= \
    SSHWIFTY_DOCKER_TLSCERTKEY= \
    SSHWIFTY_PRESETS= \
    SSHWIFTY_SERVERMESSAGE= \
    SSHWIFTY_ONLYALLOWPRESETREMOTES=
COPY --from=builder /sshwifty /
COPY . /sshwifty-src
RUN set -ex && \
    adduser -D sshwifty && \
    chmod +x /sshwifty && \
    echo '#!/bin/sh' > /sshwifty.sh && echo '([ -z "$SSHWIFTY_DOCKER_TLSCERT" ] || echo "$SSHWIFTY_DOCKER_TLSCERT" > /tmp/cert); ([ -z "$SSHWIFTY_DOCKER_TLSCERTKEY" ] || echo "$SSHWIFTY_DOCKER_TLSCERTKEY" > /tmp/certkey); if [ -f "/tmp/cert" ] && [ -f "/tmp/certkey" ]; then SSHWIFTY_TLSCERTIFICATEFILE=/tmp/cert SSHWIFTY_TLSCERTIFICATEKEYFILE=/tmp/certkey /sshwifty; else /sshwifty; fi;' >> /sshwifty.sh && chmod +x /sshwifty.sh
USER sshwifty
EXPOSE 8182
ENTRYPOINT [ "/sshwifty.sh" ]
CMD []
