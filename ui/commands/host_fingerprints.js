import { HOSTNAME, parseHostPort } from "./address.js";

function storageKey(host) {
  const endpoint = parseHostPort(host, 22);
  const addr =
    endpoint.type === HOSTNAME
      ? new TextDecoder().decode(endpoint.address).toLowerCase()
      : Array.from(endpoint.address);
  return "sshwifty-host-fingerprint:" + JSON.stringify([addr, endpoint.port]);
}

export function loadFingerprint(host) {
  try {
    return localStorage.getItem(storageKey(host)) || "";
  } catch {
    console.warn("Unable to read saved SSH host fingerprint");
    return "";
  }
}

// Called only after explicit host confirmation, never on receipt of a new key.
export function rememberFingerprint(host, fingerprint) {
  try {
    localStorage.setItem(storageKey(host), fingerprint);
    return true;
  } catch {
    return false;
  }
}

// Preset expectations are separate from browser history. Existing history is
// migrated only when no endpoint-level trust exists, never over a newer choice.
export function resolveFingerprint(host, legacyFingerprint, presetFingerprint) {
  if (presetFingerprint) return presetFingerprint;
  const current = loadFingerprint(host);
  if (current) return current;
  if (legacyFingerprint) {
    rememberFingerprint(host, legacyFingerprint);
    return legacyFingerprint;
  }
  return "";
}
