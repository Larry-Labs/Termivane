export const RECOVERY_STORAGE_KEY = "sshwifty_auto_reconnect_tabs";
const LEGACY_KEY = "sshwifty_auto_reconnect";
const MAX_AGE = 12 * 60 * 60 * 1000;

// Migrate once; never extend a saved token's lifetime while reading it.
export function readRecoveryEntries(storage, now = Date.now()) {
  const current = storage.getItem(RECOVERY_STORAGE_KEY);
  let entries;
  if (current !== null && current !== undefined) {
    entries = JSON.parse(current);
  } else {
    const legacy = storage.getItem(LEGACY_KEY);
    entries = legacy ? [JSON.parse(legacy)] : [];
  }
  const valid = (Array.isArray(entries) ? entries : []).filter(
    (entry) =>
      entry &&
      typeof entry.token === "string" &&
      entry.token &&
      typeof entry.ts === "number" &&
      now >= entry.ts &&
      now - entry.ts <= MAX_AGE &&
      entry.data &&
      typeof entry.data.host === "string" &&
      typeof entry.data.user === "string",
  );
  storage.setItem(RECOVERY_STORAGE_KEY, JSON.stringify(valid));
  storage.removeItem(LEGACY_KEY);
  return valid;
}
