import assert from "assert";
import {
  loadFingerprint,
  rememberFingerprint,
  resolveFingerprint,
} from "./host_fingerprints.js";

describe("SSH host fingerprints", () => {
  let original;
  beforeEach(() => {
    original = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
    const data = new Map();
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: {
        getItem: (key) => data.get(key) || null,
        setItem: (key, value) => data.set(key, value),
      },
    });
  });
  afterEach(() => {
    if (original) Object.defineProperty(globalThis, "localStorage", original);
    else delete globalThis.localStorage;
  });
  it("reuses an explicitly remembered key across default port spelling", () => {
    assert.strictEqual(loadFingerprint("example.com"), "");
    assert.strictEqual(
      rememberFingerprint("example.com", "SHA256:first"),
      true,
    );
    assert.strictEqual(loadFingerprint("EXAMPLE.com:22"), "SHA256:first");
    assert.strictEqual(loadFingerprint("example.com:2222"), "");
    assert.strictEqual(loadFingerprint("other.example.com"), "");
  });
  it("retains the trusted key until another key is explicitly remembered", () => {
    rememberFingerprint("127.0.0.1", "SHA256:first");
    assert.notStrictEqual(loadFingerprint("127.0.0.1"), "SHA256:changed");
    rememberFingerprint("127.0.0.1:22", "SHA256:changed");
    assert.strictEqual(loadFingerprint("127.0.0.1"), "SHA256:changed");
  });
  it("reports failure when browser storage is unavailable", () => {
    localStorage.setItem = () => {
      throw new Error("quota");
    };
    assert.strictEqual(
      rememberFingerprint("example.com", "SHA256:first"),
      false,
    );
    assert.strictEqual(loadFingerprint("example.com"), "");
  });
});

describe("Fingerprint source precedence", () => {
  let original;
  beforeEach(() => {
    original = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
    const data = new Map();
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: {
        getItem: (k) => data.get(k) || null,
        setItem: (k, v) => data.set(k, v),
      },
    });
  });
  afterEach(() => {
    if (original) Object.defineProperty(globalThis, "localStorage", original);
    else delete globalThis.localStorage;
  });
  it("uses the latest endpoint trust rather than stale per-user history", () => {
    rememberFingerprint("fixture", "new-confirmed");
    assert.strictEqual(
      resolveFingerprint("fixture:22", "old-history", ""),
      "new-confirmed",
    );
  });
  it("migrates legacy history once and preserves explicit preset expectations", () => {
    assert.strictEqual(
      resolveFingerprint("fixture", "old-history", ""),
      "old-history",
    );
    rememberFingerprint("fixture", "new-confirmed");
    assert.strictEqual(
      resolveFingerprint("fixture", "old-history", "admin-pin"),
      "admin-pin",
    );
    assert.strictEqual(loadFingerprint("fixture"), "new-confirmed");
  });
});
