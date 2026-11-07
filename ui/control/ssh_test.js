import assert from "assert";
import { describe, it } from "mocha";
import { SSH } from "./ssh.js";
import { Reader, Multiple, Limited } from "../stream/reader.js";

function frame(bytes) {
  const reader = new Reader(new Multiple(() => {}), (b) => b);
  reader.feed(bytes);
  return new Limited(reader, bytes.length);
}

describe("SSH terminal output", () => {
  it("keeps decoder state and queued output separate across stdout and stderr", async () => {
    const events = {};
    const ssh = new SSH({ get: () => ({ forget() {} }) });
    const control = ssh.build({
      charset: "utf-8",
      send() {},
      close() {},
      resize() {},
      events: {
        place: (name, handler) => {
          events[name] = handler;
        },
      },
    });
    await events.stdout(frame(new Uint8Array([0xe4, 0xb8])));
    await events.stderr(frame(new TextEncoder().encode("error\n")));
    await events.stdout(frame(new Uint8Array([0xad])));
    for (let i = 0; i < 300; i++) {
      await events.stdout(frame(new TextEncoder().encode(`${i}\n`)));
    }
    events.completed();
    assert.strictEqual(await control.receive(), "error\n");
    assert.strictEqual(await control.receive(), "中");
    for (let i = 0; i < 300; i++) {
      assert.strictEqual(await control.receive(), `${i}\n`);
    }
    await assert.rejects(async () => control.receive());
  });
});
