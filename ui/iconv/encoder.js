import iconv from "iconv-lite";

export class IconvEncoder {
  constructor(output, charset) {
    this.output = output;
    this.encoder = iconv.getEncoder(charset);
    this.closed = false;
    this.pending = "";
  }

  write(text) {
    if (this.closed) return;
    text = this.pending + text;
    this.pending = "";
    const last = text.charCodeAt(text.length - 1);
    if (last >= 0xd800 && last <= 0xdbff) {
      this.pending = text.slice(-1);
      text = text.slice(0, -1);
    }
    const bytes = this.encoder.write(text);
    if (bytes.length > 0) return this.output(bytes);
  }

  close() {
    if (this.closed) return;
    this.closed = true;
    if (this.pending) this.output(this.encoder.write(this.pending));
    this.pending = "";
    const bytes = this.encoder.end();
    if (bytes && bytes.length > 0) return this.output(bytes);
  }
}
