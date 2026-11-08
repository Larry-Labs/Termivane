import assert from "assert";
import {
  readRecoveryEntries,
  RECOVERY_STORAGE_KEY,
} from "./recovery_storage.js";

function storage(values) {
  const data = new Map(Object.entries(values));
  return {
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => data.set(key, value),
    removeItem: (key) => data.delete(key),
  };
}
const oldKey = "sshwifty_auto_reconnect";
const now = 100000000;
const record = {
  token: "fixture-token",
  data: { host: "fixture:22", user: "tester", authentication: "Private Key" },
  ts: now - 1000,
};

describe("Recovery storage migration", () => {
  it("migrates legacy records once, keeping their absolute timestamp", () => {
    const store = storage({ [oldKey]: JSON.stringify(record) });
    assert.deepStrictEqual(readRecoveryEntries(store, now), [record]);
    assert.strictEqual(store.getItem(oldKey), null);
    assert.deepStrictEqual(JSON.parse(store.getItem(RECOVERY_STORAGE_KEY)), [
      record,
    ]);
    assert.deepStrictEqual(readRecoveryEntries(store, now + 2000), [record]);
  });
  it("never resurrects the legacy tab when a current empty list exists", () => {
    const store = storage({
      [oldKey]: JSON.stringify(record),
      [RECOVERY_STORAGE_KEY]: "[]",
    });
    assert.deepStrictEqual(readRecoveryEntries(store, now), []);
    assert.strictEqual(store.getItem(oldKey), null);
  });
  it("rejects expired, malformed and future entries without extending the deadline", () => {
    const store = storage({
      [RECOVERY_STORAGE_KEY]: JSON.stringify([
        null,
        {},
        { ...record, ts: now - 12 * 60 * 60 * 1000 - 1 },
        { ...record, ts: now + 1 },
        record,
      ]),
    });
    assert.deepStrictEqual(readRecoveryEntries(store, now), [record]);
  });
});
