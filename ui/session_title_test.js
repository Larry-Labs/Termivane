import assert from "assert";
import Vue from "vue";
import { sessionPageTitle } from "./session_title.js";

describe("Browser session title", () => {
  it("follows reactive selection, disconnection and closing without output indicators", async () => {
    const tabs = [
      {
        name: "阿里云主 VPS",
        status: { disconnected: false },
        indicator: { updated: false },
      },
      {
        name: "root@182.92.78.194",
        status: { disconnected: false },
        indicator: { updated: false },
      },
    ];
    const vm = new Vue({
      data: { tabs, current: -1 },
      computed: {
        title() {
          return sessionPageTitle(this.tabs[this.current]);
        },
      },
    });
    assert.strictEqual(vm.title, "Termivane");
    vm.current = 0;
    assert.strictEqual(vm.title, "阿里云主 VPS · Termivane");
    vm.current = 1;
    assert.strictEqual(vm.title, "root@182.92.78.194 · Termivane");
    tabs[0].indicator.updated = true;
    assert.strictEqual(vm.title, "root@182.92.78.194 · Termivane");
    tabs[1].status.disconnected = true;
    assert.strictEqual(vm.title, "已断开 · root@182.92.78.194 · Termivane");
    vm.tabs.splice(1, 1);
    vm.current = 0;
    assert.strictEqual(vm.title, "阿里云主 VPS · Termivane");
    vm.tabs.splice(0, 1);
    vm.current = -1;
    assert.strictEqual(vm.title, "Termivane");
    vm.$destroy();
  });
});
