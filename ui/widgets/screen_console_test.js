import assert from "assert";
import fs from "node:fs";

describe("SFTP panel terminal layout", () => {
  it("uses the component fit method after showing or hiding files", () => {
    const source = fs.readFileSync(
      new URL("./screen_console.vue", import.meta.url),
      "utf8",
    );
    const method = source.slice(
      source.indexOf("    toggleFilePanel() {"),
      source.indexOf("    startResize(e) {"),
    );
    const component = Function("return ({" + method + "})")();
    const callbacks = [];
    let fits = 0,
      refreshes = 0;
    Object.assign(component, {
      filePanel: false,
      files: [],
      refreshFiles() {
        refreshes++;
      },
      fit() {
        fits++;
      },
      $nextTick(fn) {
        callbacks.push(fn);
      },
    });
    component.toggleFilePanel();
    assert.strictEqual(component.filePanel, true);
    callbacks.shift()();
    component.toggleFilePanel();
    callbacks.shift()();
    assert.strictEqual(component.filePanel, false);
    assert.strictEqual(fits, 2);
    assert.strictEqual(refreshes, 1);
  });
});
