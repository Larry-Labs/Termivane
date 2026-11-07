// UTF-7's shifted base64 sections may span any number of protocol frames.
// Keep the shift and UTF-16 decoder state independently of frame boundaries.
export class UTF7Decoder {
  constructor() {
    this.shifted = false;
    this.base64 = "";
    this.hadBase64 = false;
    this.unicode = null;
    this.atStart = true;
  }

  decodeBase64() {
    if (!this.base64) return "";
    const bytes = Uint8Array.from(atob(this.base64), (c) => c.charCodeAt(0));
    this.base64 = "";
    return this.unicode.decode(bytes, { stream: true });
  }

  finishShift() {
    let text = "";
    // A one-character suffix cannot encode a byte; signal malformed input.
    if (this.base64.length === 1) {
      text = "�";
      this.base64 = "";
    } else {
      text = this.decodeBase64();
    }
    text += this.unicode.decode();
    this.shifted = false;
    this.unicode = null;
    return text;
  }

  write(bytes) {
    let text = "";
    for (const byte of bytes) {
      const c = String.fromCharCode(byte);
      if (this.shifted) {
        if (/[A-Za-z0-9+/]/.test(c)) {
          this.hadBase64 = true;
          this.base64 += c;
          if (this.base64.length === 4) text += this.decodeBase64();
          continue;
        }
        const literalPlus = !this.hadBase64 && c === "-";
        text += this.finishShift();
        if (literalPlus) text += "+";
        if (c === "-") continue;
      }
      if (c === "+") {
        this.shifted = true;
        this.hadBase64 = false;
        this.unicode = new TextDecoder("utf-16be", { ignoreBOM: true });
      } else {
        text += byte < 128 ? c : "�";
      }
    }
    return this.stripInitialBOM(text);
  }

  end() {
    return this.stripInitialBOM(this.shifted ? this.finishShift() : "");
  }

  stripInitialBOM(text) {
    if (this.atStart && text.length > 0) {
      this.atStart = false;
      return text.charCodeAt(0) === 0xfeff ? text.slice(1) : text;
    }
    return text;
  }
}
