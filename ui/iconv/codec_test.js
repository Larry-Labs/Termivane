import assert from "assert";
import { describe, it } from "mocha";
import iconv from "iconv-lite";
import { IconvDecoder } from "./decoder.js";
import { IconvEncoder } from "./encoder.js";

describe("Terminal streaming codec", () => {
  for (const charset of [
    "utf-8",
    "gb18030",
    "utf-16le",
    "utf-32le",
    "utf-32be",
    "utf-7",
  ]) {
    it(`decodes every possible byte boundary in ${charset}`, () => {
      const text = "中文 terminal 😀\r\n";
      const encoded = iconv.encode(text, charset);
      for (let i = 1; i < encoded.length; i++) {
        const out = [];
        const decoder = new IconvDecoder((s) => out.push(s), charset);
        decoder.write(encoded.subarray(0, i));
        decoder.write(encoded.subarray(i));
        decoder.close();
        assert.strictEqual(out.join(""), text, `split at ${i}`);
      }
    });
  }

  it("flushes an incomplete character exactly once on close", () => {
    const out = [];
    const decoder = new IconvDecoder((s) => out.push(s), "utf-8");
    decoder.write(new Uint8Array([0xe4, 0xb8]));
    assert.deepStrictEqual(out, []);
    decoder.close();
    decoder.close();
    decoder.write(new Uint8Array([0xad]));
    assert.deepStrictEqual(out, ["�"]);
  });

  it("handles UTF-7 shifts, literal plus and embedded BOM one byte at a time", () => {
    const text = "+中文😀+\ufeff tail";
    const out = [];
    const decoder = new IconvDecoder((s) => out.push(s), "utf-7");
    for (const byte of iconv.encode("\ufeff" + text, "utf-7")) {
      decoder.write(new Uint8Array([byte]));
    }
    decoder.close();
    assert.strictEqual(out.join(""), text);
  });

  it("decodes a UTF-7 implicit shift terminator and reports malformed tails", () => {
    const out = [];
    const decoder = new IconvDecoder((s) => out.push(s), "utf-7");
    decoder.write(Buffer.from("+Ti0 word+A"));
    decoder.close();
    assert.strictEqual(out.join(""), "中 word�");
  });

  it("reports an incomplete UTF-32 code unit on close", () => {
    const out = [];
    const decoder = new IconvDecoder((s) => out.push(s), "utf-32be");
    decoder.write(new Uint8Array([0, 0]));
    decoder.close();
    assert.deepStrictEqual(out, ["�"]);
  });

  it("encodes split UTF-16 surrogate pairs without replacement", () => {
    const out = [];
    const encoder = new IconvEncoder((b) => out.push(b), "utf-8");
    encoder.write("中\ud83d");
    encoder.write("\ude00文");
    encoder.close();
    assert.strictEqual(Buffer.concat(out).toString("utf-8"), "中😀文");
  });
});
