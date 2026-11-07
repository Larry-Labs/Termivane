import assert from "assert";
import { describe, it, beforeEach, afterEach } from "mocha";
import { ClipboardProvider } from "./clipboard.js";

describe("OSC 52 clipboard consent", () => {
  let provider;
  let state;
  let writes;
  beforeEach(() => {
    writes = [];
    state = {};
    provider = new ClipboardProvider(
      (s) => {
        state = s;
      },
      async (text) => {
        writes.push(text);
      },
    );
  });
  afterEach(() => provider.dispose());

  it("requires consent and never reads the system clipboard", async () => {
    provider.writeText("c", "remote text");
    assert.strictEqual(state.pending, true);
    assert.deepStrictEqual(writes, []);
    assert.strictEqual(await provider.readText("c"), "");
    provider.reject();
    await provider.allow();
    assert.deepStrictEqual(writes, []);
  });

  it("copies once, keeps a pending request stable and asks again", async () => {
    provider.writeText("c", "first");
    provider.writeText("c", "replacement");
    await provider.allow();
    assert.deepStrictEqual(writes, ["first"]);
    provider.writeText("c", "second");
    assert.strictEqual(state.pending, true);
    assert.deepStrictEqual(writes, ["first"]);
  });

  it("allows only this terminal until permission is revoked", async () => {
    provider.writeText("c", "first");
    await provider.allow(true);
    await provider.writeText("c", "second");
    assert.deepStrictEqual(writes, ["first", "second"]);
    provider.revoke();
    provider.writeText("c", "third");
    assert.strictEqual(state.pending, true);
    assert.strictEqual(state.allowed, false);
    assert.deepStrictEqual(writes, ["first", "second"]);
  });

  it("handles asynchronous clipboard permission failure", async () => {
    provider.write = async () => {
      throw new Error("denied");
    };
    provider.writeText("c", "first");
    await provider.allow(true);
    assert.strictEqual(state.allowed, false);
    assert.match(state.error, /Unable to copy/);
  });

  it("does not copy or retain permission after disposal", async () => {
    provider.writeText("c", "first");
    provider.dispose();
    await provider.allow(true);
    provider.writeText("c", "second");
    assert.deepStrictEqual(writes, []);
    assert.strictEqual(provider.allowed, false);
    assert.strictEqual(provider.pending, null);
  });
});
