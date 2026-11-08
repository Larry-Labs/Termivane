import assert from "assert";
import { SFTPClient } from "./client.js";

const tick = () => new Promise((r) => setImmediate(r));
async function outcome(p) {
  let result = "pending";
  p.then(
    () => {
      result = "resolved";
    },
    () => {
      result = "rejected";
    },
  );
  await tick();
  return result;
}

describe("SFTP client lifecycle", () => {
  let workers, savedWorker, savedLocation;
  beforeEach(() => {
    workers = [];
    savedWorker = Object.getOwnPropertyDescriptor(globalThis, "Worker");
    savedLocation = Object.getOwnPropertyDescriptor(globalThis, "location");
    Object.defineProperty(globalThis, "Worker", {
      configurable: true,
      value: class {
        constructor() {
          workers.push(this);
          this.sent = [];
        }
        postMessage(m) {
          this.sent.push(m);
        }
        terminate() {
          this.terminated = true;
        }
        emit(m) {
          this.onmessage?.({ data: m });
        }
      },
    });
    Object.defineProperty(globalThis, "location", {
      configurable: true,
      value: { host: "localhost", protocol: "http:" },
    });
  });
  afterEach(() => {
    if (savedWorker) Object.defineProperty(globalThis, "Worker", savedWorker);
    else delete globalThis.Worker;
    if (savedLocation)
      Object.defineProperty(globalThis, "location", savedLocation);
    else delete globalThis.location;
  });
  it("shares an in-flight connect rather than replacing its worker", async () => {
    const client = new SFTPClient();
    const a = client.connect("fixture"),
      b = client.connect("fixture");
    assert.strictEqual(workers.length, 1);
    workers[0].emit({ type: "connected" });
    assert.strictEqual(await outcome(a), "resolved");
    assert.strictEqual(await outcome(b), "resolved");
    client.close();
  });
  it("rejects pending connection on explicit close", async () => {
    const client = new SFTPClient();
    const p = client.connect("fixture");
    client.close();
    assert.strictEqual(await outcome(p), "rejected");
  });
  it("rejects current and queued operations on close", async () => {
    const client = new SFTPClient();
    const p = client.connect("fixture");
    workers[0].emit({ type: "connected" });
    await p;
    const a = client.list("/"),
      b = client.list("/other");
    const results = Promise.allSettled([a, b]);
    await tick();
    client.close();
    assert.strictEqual(await outcome(results), "resolved");
    assert((await results).every((r) => r.status === "rejected"));
  });
  it("does not send a delayed file read through a later connection", async () => {
    const client = new SFTPClient();
    const p = client.connect("fixture");
    workers[0].emit({ type: "connected" });
    await p;
    let finish;
    const upload = client.upload("/fixture", {
      size: 1,
      arrayBuffer: () =>
        new Promise((r) => {
          finish = r;
        }),
    });
    const result = upload.catch(() => {});
    await tick();
    client.close();
    const next = client.connect("fixture");
    workers[1].emit({ type: "connected" });
    await next;
    finish(new ArrayBuffer(1));
    await tick();
    assert(!workers[1].sent.some((m) => m.cmd === "upload"));
    assert.strictEqual(await outcome(result), "resolved");
    client.close();
  });
  it("ignores late worker messages after closure and rejects work on worker failure", async () => {
    const client = new SFTPClient();
    const p = client.connect("fixture");
    const worker = workers[0];
    worker.emit({ type: "connected" });
    await p;
    const pending = client.list("/");
    const result = pending.catch(() => {});
    await tick();
    worker.onerror({ message: "fixture failure" });
    assert.strictEqual(await outcome(result), "resolved");
    client.close();
    worker.emit({ type: "connected" });
    assert.strictEqual(client.isConnected(), false);
  });
  it("retries a lost connection serially and stops retries after explicit close", async () => {
    const originalSet = globalThis.setTimeout,
      originalClear = globalThis.clearTimeout;
    const timers = new Map();
    let timerID = 0;
    globalThis.setTimeout = (fn) => {
      const id = ++timerID;
      timers.set(id, fn);
      return id;
    };
    globalThis.clearTimeout = (id) => timers.delete(id);
    const client = new SFTPClient();
    try {
      const p = client.connect("fixture");
      workers[0].emit({ type: "connected" });
      await p;
      workers[0].emit({ type: "disconnected" });
      assert.strictEqual(timers.size, 1);
      const retry = [...timers.values()][0];
      timers.clear();
      retry();
      assert.strictEqual(workers.length, 2);
      workers[1].emit({ type: "connectError", error: "temporary" });
      await tick();
      assert.strictEqual(timers.size, 1);
      client.close();
      assert.strictEqual(timers.size, 0);
      workers[1].emit({ type: "connected" });
      assert.strictEqual(client.isConnected(), false);
    } finally {
      client.close();
      globalThis.setTimeout = originalSet;
      globalThis.clearTimeout = originalClear;
    }
  });
});
