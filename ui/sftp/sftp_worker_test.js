import assert from "assert";
import fs from "node:fs";
import vm from "node:vm";

function fixture() {
  const sent = [],
    messages = [],
    timers = [];
  let socket;
  class WebSocket {
    static OPEN = 1;
    constructor() {
      socket = this;
      this.readyState = 1;
      this.bufferedAmount = 0;
    }
    send(value) {
      sent.push(typeof value === "string" ? JSON.parse(value) : value);
    }
    close() {
      this.readyState = 3;
      this.onclose?.();
    }
  }
  const context = vm.createContext({
    WebSocket,
    Blob,
    Date,
    self: { postMessage: (m) => messages.push(m) },
    setInterval: () => 1,
    clearInterval() {},
    setTimeout: (fn) => timers.push(fn),
  });
  vm.runInContext(
    fs.readFileSync(new URL("./sftp.worker.js", import.meta.url), "utf8"),
    context,
  );
  const command = (data) => context.self.onmessage({ data });
  command({
    cmd: "connect",
    sessionID: "fixture",
    origin: "localhost",
    protocol: "ws:",
  });
  const receive = (data) => socket.onmessage({ data: JSON.stringify(data) });
  return { sent, messages, command, receive, socket, timers };
}

describe("SFTP worker protocol", () => {
  it("requires upload ready and final result; heartbeat cannot advance either stage", () => {
    const f = fixture();
    f.receive({ type: "success", message: "connected" });
    f.command({
      cmd: "upload",
      id: "upload",
      path: "/fixture",
      fileData: new ArrayBuffer(1),
      fileSize: 1,
    });
    f.receive({ type: "pong" });
    assert.strictEqual(f.sent.length, 1);
    f.receive({ type: "success", message: "ready" });
    assert.strictEqual(f.sent.length, 3);
    f.receive({ type: "pong" });
    assert.strictEqual(
      f.messages.filter((m) => m.type === "response").length,
      0,
    );
    f.receive({ type: "success", message: "ok" });
    assert.strictEqual(
      f.messages.filter((m) => m.type === "response").length,
      1,
    );
  });
  it("settles a connection closed before application handshake", () => {
    const f = fixture();
    f.socket.close();
    assert(f.messages.some((m) => m.type === "connectError"));
  });
  it("rejects unexpected operation replies rather than reporting success", () => {
    const f = fixture();
    f.receive({ type: "success", message: "connected" });
    f.command({
      cmd: "request",
      id: "list",
      payload: { action: "list", path: "/" },
    });
    f.receive({ type: "success", message: "ready" });
    assert(f.messages.some((m) => m.type === "response" && m.error));
  });
  it("reports closure during chunked upload and does not send delayed chunks", () => {
    const f = fixture();
    f.receive({ type: "success", message: "connected" });
    f.command({
      cmd: "upload",
      id: "upload",
      path: "/fixture",
      fileData: new ArrayBuffer(1024 * 1024),
      fileSize: 1024 * 1024,
    });
    f.receive({ type: "success", message: "ready" });
    f.socket.close();
    const count = f.sent.length;
    for (const fn of f.timers) fn();
    assert.strictEqual(f.sent.length, count);
    assert(
      f.messages.some(
        (m) => m.type === "response" && m.id === "upload" && m.error,
      ),
    );
  });
  it("keeps heartbeats separate from successful list, mutation and download results", async () => {
    const f = fixture();
    f.receive({ type: "success", message: "connected" });
    f.command({
      cmd: "request",
      id: "list",
      payload: { action: "list", path: "/" },
    });
    f.receive({ type: "pong" });
    f.receive({ type: "list", files: [{ name: "fixture" }] });
    f.command({
      cmd: "request",
      id: "mkdir",
      payload: { action: "mkdir", path: "/fixture" },
    });
    f.receive({ type: "pong" });
    f.receive({ type: "success", message: "ok" });
    f.command({ cmd: "download", id: "download", path: "/fixture" });
    f.receive({ type: "download_start", total: 3 });
    f.socket.onmessage({ data: new Uint8Array([1, 2, 3]).buffer });
    f.receive({ type: "pong" });
    f.receive({ type: "download_end" });
    const replies = f.messages.filter((m) => m.type === "response");
    assert.deepStrictEqual(
      replies.map((m) => m.id),
      ["list", "mkdir", "download"],
    );
    assert.deepStrictEqual(
      [...new Uint8Array(await replies[2].result.arrayBuffer())],
      [1, 2, 3],
    );
  });
  it("reports initial server rejection without sending upload data", () => {
    const f = fixture();
    f.receive({ type: "success", message: "connected" });
    f.command({
      cmd: "upload",
      id: "upload",
      path: "/fixture",
      fileData: new ArrayBuffer(1),
      fileSize: 1,
    });
    f.receive({ type: "error", message: "permission denied" });
    assert.strictEqual(f.sent.length, 1);
    assert(
      f.messages.some(
        (m) => m.id === "upload" && m.error === "permission denied",
      ),
    );
  });
  it("rejects invalid download sizes", () => {
    for (const total of [null, -1, 0.5, "3", Number.MAX_SAFE_INTEGER + 1]) {
      const f = fixture();
      f.receive({ type: "success", message: "connected" });
      f.command({ cmd: "download", id: "download", path: "/fixture" });
      f.receive({ type: "download_start", total });
      assert(
        f.messages.some((m) => m.id === "download" && m.error),
        `accepted invalid size ${total}`,
      );
      assert.strictEqual(f.socket.readyState, 3);
    }
  });
  it("accepts an empty download only when its declared size is zero", async () => {
    const f = fixture();
    f.receive({ type: "success", message: "connected" });
    f.command({ cmd: "download", id: "download", path: "/fixture" });
    f.receive({ type: "download_start", total: 0 });
    f.receive({ type: "download_end" });
    const reply = f.messages.find((m) => m.id === "download");
    assert.strictEqual(reply.error, undefined);
    assert.strictEqual(reply.result.size, 0);
  });
  it("rejects a truncated download even when the server reports completion", () => {
    const f = fixture();
    f.receive({ type: "success", message: "connected" });
    f.command({ cmd: "download", id: "download", path: "/fixture" });
    f.receive({ type: "download_start", total: 3 });
    f.socket.onmessage({ data: new Uint8Array([1, 2]).buffer });
    f.receive({ type: "download_end" });
    assert(f.messages.some((m) => m.id === "download" && m.error));
    assert.strictEqual(f.socket.readyState, 3);
  });
  it("rejects overlong downloads as soon as data exceeds the declared size", () => {
    const f = fixture();
    f.receive({ type: "success", message: "connected" });
    f.command({ cmd: "download", id: "download", path: "/fixture" });
    f.receive({ type: "download_start", total: 1 });
    f.socket.onmessage({ data: new Uint8Array([1, 2]).buffer });
    assert(f.messages.some((m) => m.id === "download" && m.error));
    assert.strictEqual(f.socket.readyState, 3);
  });

  it("accepts the legacy omitted zero size but rejects any undeclared bytes", () => {
    for (const hasBytes of [false, true]) {
      const f = fixture();
      f.receive({ type: "success", message: "connected" });
      f.command({ cmd: "download", id: "download", path: "/fixture" });
      f.receive({ type: "download_start" });
      if (hasBytes) f.socket.onmessage({ data: new Uint8Array([1]).buffer });
      else f.receive({ type: "download_end" });
      const reply = f.messages.find((m) => m.id === "download");
      if (hasBytes) assert(reply.error);
      else assert.strictEqual(reply.result.size, 0);
    }
  });
});
