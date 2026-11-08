let ws = null;
let connected = false;
let operation = null;
let heartbeat = null;

function sendToMain(msg) {
  self.postMessage(msg);
}

function stopHeartbeat() {
  if (heartbeat !== null) clearInterval(heartbeat);
  heartbeat = null;
}

function startHeartbeat() {
  stopHeartbeat();
  heartbeat = setInterval(() => {
    if (connected && ws?.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ action: "ping" }));
    }
  }, 30000);
}

function finishOperation(result, error) {
  if (!operation) return;
  const id = operation.id;
  operation = null;
  sendToMain({ type: "response", id, result, error });
}

function protocolError(message = "Unexpected SFTP response") {
  finishOperation(undefined, message);
  // The server protocol is ordered, without request IDs. Once ordering is
  // invalid, discard this transport instead of assigning replies to later work.
  ws?.close();
}

function handleTextMessage(text) {
  let msg;
  try {
    msg = JSON.parse(text);
  } catch {
    protocolError();
    return;
  }
  if (!msg || typeof msg !== "object") {
    protocolError();
    return;
  }
  if (msg.type === "pong") return;

  if (!connected) {
    if (msg.type === "success" && msg.message === "connected") {
      connected = true;
      startHeartbeat();
      sendToMain({ type: "connected" });
    } else {
      sendToMain({
        type: "connectError",
        error: msg.message || "Unexpected SFTP handshake response",
      });
      ws?.close();
    }
    return;
  }

  if (!operation) {
    protocolError();
    return;
  }
  if (msg.type === "error") {
    finishOperation(undefined, msg.message || "SFTP operation failed");
    return;
  }
  switch (operation.stage) {
    case "list":
      if (msg.type === "list") {
        finishOperation(msg);
        return;
      }
      break;
    case "mutation":
    case "upload-result":
      if (msg.type === "success" && msg.message === "ok") {
        finishOperation(msg);
        return;
      }
      break;
    case "upload-ready":
      if (msg.type === "success" && msg.message === "ready") {
        operation.stage = "upload-stream";
        sendUpload(operation);
        return;
      }
      break;
    case "download-start":
      if (msg.type === "download_start") {
        // Older backends omit a zero total; no received bytes are allowed then.
        const total = msg.total === undefined ? 0 : msg.total;
        if (!Number.isSafeInteger(total) || total < 0) {
          protocolError("Invalid SFTP download size");
          return;
        }
        operation.total = total;
        operation.received = 0;
        operation.stage = "download-stream";
        return;
      }
      break;
    case "download-stream":
      if (msg.type === "download_end") {
        if (operation.received !== operation.total) {
          protocolError("Incomplete SFTP download");
          return;
        }
        finishOperation(new Blob(operation.chunks));
        return;
      }
      break;
  }
  protocolError();
}

function handleBinaryMessage(data) {
  if (operation?.stage !== "download-stream") {
    protocolError();
    return;
  }
  if (data.byteLength > operation.total - operation.received) {
    protocolError("SFTP download exceeds declared size");
    return;
  }
  operation.received += data.byteLength;
  operation.chunks.push(data);
}

function doConnect(sessionID, origin, protocol) {
  const socket = new WebSocket(
    `${protocol}//${origin}/sshwifty/sftp?session=${sessionID}`,
  );
  ws = socket;
  connected = false;
  socket.binaryType = "arraybuffer";
  socket.onmessage = (ev) => {
    if (ws !== socket) return;
    if (typeof ev.data === "string") handleTextMessage(ev.data);
    else handleBinaryMessage(ev.data);
  };
  socket.onerror = () => {
    if (ws !== socket) return;
    if (!connected)
      sendToMain({
        type: "connectError",
        error: "SFTP WebSocket connection failed",
      });
    socket.close();
  };
  socket.onclose = () => {
    if (ws !== socket) return;
    const wasConnected = connected;
    connected = false;
    ws = null;
    stopHeartbeat();
    finishOperation(undefined, "SFTP connection closed");
    sendToMain(
      wasConnected
        ? { type: "disconnected" }
        : {
            type: "connectError",
            error: "SFTP connection closed before handshake",
          },
    );
  };
}

function beginOperation(id, stage, fields = {}) {
  if (!connected || ws?.readyState !== WebSocket.OPEN) {
    sendToMain({ type: "response", id, error: "SFTP not connected" });
    return false;
  }
  if (operation) {
    sendToMain({
      type: "response",
      id,
      error: "SFTP operation already in progress",
    });
    return false;
  }
  operation = { id, stage, ...fields };
  return true;
}

function doRequest(id, obj) {
  if (beginOperation(id, obj.action === "list" ? "list" : "mutation")) {
    ws.send(JSON.stringify(obj));
  }
}

function sendUpload(upload) {
  // A close/error invalidates the operation, including scheduled drain work.
  if (operation !== upload || !connected || ws?.readyState !== WebSocket.OPEN)
    return;
  if (ws.bufferedAmount >= 512 * 1024) {
    setTimeout(() => sendUpload(upload), 5);
    return;
  }
  let count = 0;
  while (upload.offset < upload.size && count++ < 8) {
    const end = Math.min(upload.offset + 64 * 1024, upload.size);
    ws.send(upload.data.slice(upload.offset, end));
    upload.offset = end;
    if (ws.bufferedAmount >= 512 * 1024) break;
  }
  const pct = upload.size
    ? Math.floor((upload.offset / upload.size) * 100)
    : 100;
  sendToMain({
    type: "uploadProgress",
    id: upload.id,
    pct,
    speed: (upload.offset / Math.max(1, Date.now() - upload.started)) * 1000,
  });
  if (upload.offset < upload.size || ws.bufferedAmount >= 512 * 1024) {
    setTimeout(() => sendUpload(upload), 5);
  } else {
    upload.stage = "upload-result";
    upload.data = null;
    ws.send(JSON.stringify({ action: "upload_done" }));
  }
}

function doUpload(id, path, data, size) {
  if (
    beginOperation(id, "upload-ready", {
      data,
      size,
      offset: 0,
      started: Date.now(),
    })
  ) {
    ws.send(JSON.stringify({ action: "upload", path, size }));
  }
}

function doDownload(id, path) {
  if (beginOperation(id, "download-start", { chunks: [] })) {
    ws.send(JSON.stringify({ action: "download", path }));
  }
}

function doClose() {
  stopHeartbeat();
  finishOperation(undefined, "SFTP connection closed");
  const socket = ws;
  ws = null;
  connected = false;
  socket?.close();
}

self.onmessage = (ev) => {
  const msg = ev.data;
  switch (msg.cmd) {
    case "connect":
      doConnect(msg.sessionID, msg.origin, msg.protocol);
      break;
    case "request":
      doRequest(msg.id, msg.payload);
      break;
    case "upload":
      doUpload(msg.id, msg.path, msg.fileData, msg.fileSize);
      break;
    case "download":
      doDownload(msg.id, msg.path);
      break;
    case "close":
      doClose();
      break;
  }
};
