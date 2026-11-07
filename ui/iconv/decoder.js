import iconv from "iconv-lite";
import { Buffer } from "buffer";
import { UTF7Decoder } from "./utf7_decoder.js";

// Retain partial multibyte characters between terminal protocol frames.
export class IconvDecoder {
  constructor(output, charset) {
    this.output = output;
    this.native = null;
    try {
      this.native = new TextDecoder(charset);
    } catch {
      // Preserve legacy encodings (for example UTF-7/UTF-32) that the
      // browser's Encoding API does not implement.
    }
    const normalized = charset.toLowerCase().replace(/[-_]/g, "");
    this.decoder = this.native
      ? null
      : normalized === "utf7"
        ? new UTF7Decoder()
        : iconv.getDecoder(charset);
    this.unitSize = /^(utf32|ucs4)/.test(
      charset.toLowerCase().replace(/[-_]/g, ""),
    )
      ? 4
      : 1;
    this.pending = Buffer.alloc(0);
    this.closed = false;
  }

  write(bytes) {
    if (this.closed) return;
    // UTF-32 codecs consume fixed four-byte code units. Frame these explicitly
    // because the browser does not provide a UTF-32 streaming decoder.
    if (this.unitSize > 1) {
      bytes = Buffer.concat([this.pending, Buffer.from(bytes)]);
      const length = bytes.length - (bytes.length % this.unitSize);
      this.pending = bytes.subarray(length);
      bytes = bytes.subarray(0, length);
    }
    const text = this.native
      ? this.native.decode(bytes, { stream: true })
      : this.decoder.write(Buffer.from(bytes));
    if (text.length > 0) this.output(text);
  }

  close() {
    if (this.closed) return;
    this.closed = true;
    const text = this.native ? this.native.decode() : this.decoder.end();
    if (text) this.output(text);
    if (this.pending.length > 0) this.output("�");
    this.pending = Buffer.alloc(0);
  }
}
