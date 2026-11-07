import assert from "assert";
import {
  parseKeyboardChallenge,
  encodeKeyboardAnswers,
} from "./keyboard_interactive.js";

describe("Keyboard-interactive authentication", () => {
  it("preserves server echo flags, Unicode prompts and ordered answers", () => {
    const value = {
      name: "认证",
      instruction: "二次验证",
      questions: ["用户", "验证码"],
      echo: [true, false],
    };
    assert.deepStrictEqual(
      parseKeyboardChallenge(new TextEncoder().encode(JSON.stringify(value))),
      value,
    );
    assert.deepStrictEqual(
      JSON.parse(
        new TextDecoder().decode(encodeKeyboardAnswers(["测试", "123456"])),
      ),
      ["测试", "123456"],
    );
  });

  it("rejects malformed challenges instead of displaying secret fields as text", () => {
    for (const echo of [[], ["false"], null]) {
      const value = { name: "", instruction: "", questions: ["Code"], echo };
      assert.throws(() =>
        parseKeyboardChallenge(new TextEncoder().encode(JSON.stringify(value))),
      );
    }
  });

  it("limits encoded byte size rather than character count", () => {
    assert.throws(() => encodeKeyboardAnswers(["中".repeat(1400)]), /too long/);
    assert.strictEqual(
      new TextDecoder().decode(encodeKeyboardAnswers([])),
      "[]",
    );
  });
});
