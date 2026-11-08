import assert from "assert";
import fs from "fs";

function method(name, next) {
  const source = fs.readFileSync(
    new URL("./home.vue", import.meta.url),
    "utf8",
  );
  const start = source.indexOf(`    ${name}(`);
  return new Function(
    `return ({${source.slice(start, source.indexOf(`    ${next}(`, start))}})`,
  )()[name.replace(/^async /, "")];
}

describe("Recovery state regressions", () => {
  it("saves explicit tab metadata and never inherits another tab by position", () => {
    const data = new Map([
      [
        "sshwifty_auto_reconnect_tabs",
        JSON.stringify([{ token: "old-A", data: { host: "A" } }]),
      ],
    ]);
    globalThis.sessionStorage = {
      getItem: (k) => data.get(k),
      setItem: (k, v) => data.set(k, v),
      removeItem: (k) => data.delete(k),
    };
    const metadata = {
      host: "B:22",
      user: "deploy",
      authentication: "Private Key",
      charset: "utf-8",
    };
    method("saveAutoReconnect", "async tryAutoReconnect").call({
      reconnecting: false,
      tab: {
        tabs: [
          {
            id: 2,
            name: "An arbitrary alias",
            connectionData: metadata,
            control: { reconnectToken: "new-B" },
          },
        ],
      },
      connector: { historyRec: { all: () => [] } },
    });
    assert.deepStrictEqual(
      JSON.parse(data.get("sshwifty_auto_reconnect_tabs"))[0].data,
      metadata,
    );
    assert.strictEqual(data.has("sshwifty_auto_reconnect"), false);
    delete globalThis.sessionStorage;
  });
  it("cancels a prompt instead of waiting for invisible user input", async () => {
    let cancelled = false,
      reads = 0;
    const wizard = {
      next: async () => {
        reads++;
        return reads === 1
          ? {
              type: () => 1,
              data: () => ({
                cancel: () => {
                  cancelled = true;
                },
              }),
            }
          : { type: () => 3, data: () => ({ success: () => false }) };
      },
      close: () => {},
    };
    const context = {
      connection: { get: async () => ({}) },
      getConnectorByType: () => ({ execute: () => wizard }),
      connector: { historyRec: { all: () => [] } },
      recoveryErrors: [],
    };
    const result = await method(
      "async connectIndependent",
      "async tryReattachSession",
    ).call(context, { type: "SSH", data: { host: "fixture" } });
    assert.strictEqual(result, null);
    assert.strictEqual(cancelled, true);
    assert.strictEqual(reads, 1);
  });
});

describe("Failed reattachment", () => {
  it("does not create a new SSH session after an unavailable reattach endpoint", async () => {
    const original = globalThis.fetch;
    const requests = [];
    globalThis.fetch = async (url) => {
      requests.push(url);
      return { ok: false };
    };
    try {
      const context = {
        connectIndependent: () => {
          throw new Error("must not establish a new connection");
        },
      };
      const result = await method(
        "async tryReattachSession",
        "async addToTab",
      ).call(context, { token: "fixture-token" });
      assert.strictEqual(result, false);
      assert.deepStrictEqual(requests, [
        "/sshwifty/session/reattach?token=fixture-token",
      ]);
    } finally {
      globalThis.fetch = original;
    }
  });
});

describe("Explicit launcher priority", () => {
  it("shows an explicit launcher even when saved sessions marked the home as restoring", () => {
    const source = fs.readFileSync(
      new URL("./home.vue", import.meta.url),
      "utf8",
    );
    const mounted = new Function(
      `return ({${source.slice(source.indexOf("  mounted() {"), source.indexOf("  beforeDestroy() {"))}})`,
    )().mounted;
    const originalWindow = globalThis.window;
    globalThis.window = { addEventListener: () => {} };
    let launched = false;
    const context = {
      reconnecting: true,
      query: "+SSH:user@fixture:22|Password|utf-8",
      tick: () => {},
      connectLaunch(launcher, callback) {
        launched = true;
        assert.strictEqual(this.reconnecting, false);
        assert.strictEqual(launcher, "SSH:user@fixture:22|Password|utf-8");
        callback(false);
      },
      tryAutoReconnect() {
        throw new Error(
          "Explicit launch must not run saved-session restoration",
        );
      },
    };
    try {
      mounted.call(context);
      assert.strictEqual(launched, true);
    } finally {
      clearInterval(context.ticker);
      if (originalWindow === undefined) delete globalThis.window;
      else globalThis.window = originalWindow;
    }
  });
});
