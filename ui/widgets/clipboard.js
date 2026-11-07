// OSC 52 clipboard support, adapted from nirui/sshwifty's consent flow.
// Permission is scoped to this terminal and is never persisted.
export class ClipboardProvider {
  constructor(changed, write = (text) => navigator.clipboard.writeText(text)) {
    this.changed = changed;
    this.write = write;
    this.pending = null;
    this.allowed = false;
    this.error = "";
    this.timer = null;
    this.closed = false;
  }

  readText() {
    // A remote terminal must never read the user's system clipboard.
    return "";
  }

  writeText(selection, text) {
    if (this.closed) return;
    if (this.allowed) return this.copy(text);
    // Keep the request the user is deciding on; do not replace it mid-prompt.
    if (this.pending !== null) return;
    this.pending = text;
    this.error = "";
    this.timer = setTimeout(() => this.reject(), 10000);
    this.publish();
  }

  publish() {
    if (!this.closed) {
      this.changed({
        pending: this.pending !== null,
        allowed: this.allowed,
        error: this.error,
      });
    }
  }

  clearPending() {
    clearTimeout(this.timer);
    this.timer = null;
    this.pending = null;
  }

  reject() {
    this.clearPending();
    this.error = "";
    this.publish();
  }

  allow(forSession = false) {
    if (this.closed || this.pending === null) return;
    const text = this.pending;
    this.clearPending();
    this.allowed = forSession;
    this.publish();
    // Invoke synchronously from the click handler to preserve user activation.
    return this.copy(text);
  }

  revoke() {
    this.allowed = false;
    this.reject();
  }

  async copy(text) {
    try {
      await this.write(text);
      this.error = "";
    } catch {
      this.allowed = false;
      this.error = "Unable to copy. Check your browser's clipboard permission.";
    }
    this.publish();
  }

  dispose() {
    this.closed = true;
    this.allowed = false;
    this.clearPending();
  }
}
