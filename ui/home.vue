<!--
// Sshwifty - A Web SSH client
//
// Copyright (C) 2019-2025 Ni Rui <ranqus@gmail.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
-->

<template>
  <div id="home" :class="{ 'home-restoring': reconnecting }">
    <header id="home-header">
      <h1 id="home-hd-title">Termivane</h1>

      <a id="home-hd-delay" href="javascript:;" @click="showDelayWindow">
        <span
          id="home-hd-delay-icon"
          class="icon icon-point1"
          :class="socket.classStyle"
        ></span>
        <span v-if="socket.message.length > 0" id="home-hd-delay-value">{{
          socket.message
        }}</span>
      </a>

      <a
        id="home-hd-plus"
        class="icon icon-plus1"
        href="javascript:;"
        :class="{
          working: connector.inputting,
          intensify: connector.inputting && !windows.connect,
        }"
        @click="showConnectWindow"
      ></a>

      <tabs
        id="home-hd-tabs"
        :tab="tab.current"
        :tabs="tab.tabs"
        tabs-class="tab1"
        list-trigger-class="icon icon-more1"
        @current="switchTab"
        @retap="retapTab"
        @list="showTabsWindow"
        @close="closeTab"
      ></tabs>
    </header>

    <screens
      id="home-content"
      :screen="tab.current"
      :screens="tab.tabs"
      :view-port="viewPort"
      @stopped="tabStopped"
      @warning="tabWarning"
      @info="tabInfo"
      @updated="tabUpdated"
    >
      <div id="home-content-wrap">
        <div v-if="reconnecting" id="home-reconnecting"></div>
        <template v-else>
          <h1>Welcome to Termivane</h1>

          <p>
            An Open Source Web SSH Client that enables you to connect to SSH
            servers without downloading any additional software.
          </p>

          <p>
            To get started, click the
            <span
              id="home-content-connect"
              class="icon icon-plus1"
              @click="showConnectWindow"
            ></span>
            icon near the top left corner.
          </p>

          <div v-if="serverMessage.length > 0">
            <hr />
            <p class="secondary" v-html="serverMessage"></p>
          </div>

          <hr />
          <p class="secondary">Maintained by Larry Gao</p>
        </template>
      </div>
    </screens>

    <connect-widget
      :inputting="connector.inputting"
      :display="windows.connect"
      :connectors="connector.connectors"
      :presets="presets"
      :restricted-to-presets="restrictedToPresets"
      :knowns="connector.knowns"
      :knowns-launcher-builder="buildknownLauncher"
      :knowns-export="exportKnowns"
      :knowns-import="importKnowns"
      :busy="connector.busy"
      @display="windows.connect = $event"
      @connector-select="connectNew"
      @known-select="connectKnown"
      @known-remove="removeKnown"
      @preset-select="connectPreset"
      @known-clear-session="clearSessionKnown"
    >
      <connector
        :connector="connector.connector"
        @cancel="cancelConnection"
        @done="connectionSucceed"
      >
      </connector>
    </connect-widget>
    <status-widget
      :class="socket.windowClass"
      :display="windows.delay"
      :status="socket.status"
      @display="windows.delay = $event"
    ></status-widget>
    <tab-window
      :tab="tab.current"
      :tabs="tab.tabs"
      :display="windows.tabs"
      tabs-class="tab1 tab1-list"
      @display="windows.tabs = $event"
      @current="switchTab"
      @retap="retapTab"
      @close="closeTab"
    ></tab-window>
  </div>
</template>

<script>
import "./home.css";

import ConnectWidget from "./widgets/connect.vue";
import StatusWidget from "./widgets/status.vue";
import Connector from "./widgets/connector.vue";
import Tabs from "./widgets/tabs.vue";
import { sessionPageTitle } from "./session_title.js";
import TabWindow from "./widgets/tab_window.vue";
import Screens from "./widgets/screens.vue";

import * as home_socket from "./home_socketctl.js";
import * as home_history from "./home_historyctl.js";

import * as presets from "./commands/presets.js";

const BACKEND_CONNECT_ERROR =
  "Unable to connect to the Termivane backend server: ";
const BACKEND_REQUEST_ERROR = "Unable to perform request: ";

export default {
  components: {
    "connect-widget": ConnectWidget,
    "status-widget": StatusWidget,
    connector: Connector,
    tabs: Tabs,
    "tab-window": TabWindow,
    screens: Screens,
  },
  props: {
    hostPath: {
      type: String,
      default: "",
    },
    query: {
      type: String,
      default: "",
    },
    connection: {
      type: Object,
      default: () => null,
    },
    controls: {
      type: Object,
      default: () => null,
    },
    commands: {
      type: Object,
      default: () => null,
    },
    serverMessage: {
      type: String,
      default: "",
    },
    presetData: {
      type: Object,
      default: () => new presets.Presets([]),
    },
    restrictedToPresets: {
      type: Boolean,
      default: () => false,
    },
    viewPort: {
      type: Object,
      default: () => null,
    },
  },
  data() {
    let history = home_history.build(this);

    let hasRecoverableSessions = false;
    try {
      const maxAge = 12 * 60 * 60 * 1000;
      const rawTabs = sessionStorage.getItem("sshwifty_auto_reconnect_tabs");
      if (rawTabs) {
        const tabs = JSON.parse(rawTabs);
        hasRecoverableSessions =
          Array.isArray(tabs) &&
          tabs.some((t) => t.token && Date.now() - (t.ts || 0) <= maxAge);
      }
      if (!hasRecoverableSessions) {
        const rawSingle = sessionStorage.getItem("sshwifty_auto_reconnect");
        if (rawSingle) {
          const single = JSON.parse(rawSingle);
          hasRecoverableSessions =
            single.token && Date.now() - (single.ts || 0) <= maxAge;
        }
      }
    } catch (_e) {
      void _e;
    }

    return {
      ticker: null,
      reconnecting: hasRecoverableSessions,
      windows: {
        delay: false,
        connect: false,
        tabs: false,
      },
      socket: home_socket.build(this),
      connector: {
        historyRec: history,
        connector: null,
        connectors: this.commands.all(),
        inputting: false,
        acquired: false,
        busy: false,
        knowns: history.all(),
      },
      presets: this.commands.mergePresets(this.presetData),
      tab: {
        current: -1,
        lastID: 0,
        tabs: [],
      },
    };
  },
  computed: {
    pageTitle() {
      return sessionPageTitle(this.tab.tabs[this.tab.current]);
    },
  },
  watch: {
    pageTitle: {
      immediate: true,
      handler(title) {
        this.$emit("title-changed", title);
      },
    },
  },
  mounted() {
    this.ticker = setInterval(() => {
      this.tick();
    }, 1000);

    if (this.query.length > 1 && this.query.indexOf("+") === 0) {
      this.connectLaunch(this.query.slice(1, this.query.length), (success) => {
        if (!success) {
          return;
        }

        this.$emit("navigate-to", "");
      });
    } else {
      this.tryAutoReconnect();
    }

    window.addEventListener("beforeunload", this.onBrowserClose);
  },
  beforeDestroy() {
    window.removeEventListener("beforeunload", this.onBrowserClose);

    if (this.ticker !== null) {
      clearInterval(this.ticker);
      this.ticker = null;
    }

    if (this._autoSaveInterval) {
      clearInterval(this._autoSaveInterval);
      this._autoSaveInterval = null;
    }
  },
  methods: {
    onBrowserClose(e) {
      this.saveAutoReconnect();
      if (this.tab.current < 0) {
        return undefined;
      }
      const msg = "Some tabs are still open, are you sure you want to exit?";
      (e || window.event).returnValue = msg;
      return msg;
    },
    tick() {
      let now = new Date();

      this.socket.update(now, this);
    },
    closeAllWindow(e) {
      for (let i in this.windows) {
        this.windows[i] = false;
      }
    },
    showDelayWindow() {
      this.closeAllWindow();
      this.windows.delay = true;
    },
    showConnectWindow() {
      this.closeAllWindow();
      this.windows.connect = true;
    },
    showTabsWindow() {
      this.closeAllWindow();
      this.windows.tabs = true;
    },
    async getStreamThenRun(run, end) {
      let errStr = null;

      try {
        let conn = await this.connection.get(this.socket);

        try {
          run(conn);
        } catch (e) {
          errStr = BACKEND_REQUEST_ERROR + e;

          process.env.NODE_ENV === "development" && console.trace(e);
        }
      } catch (e) {
        errStr = BACKEND_CONNECT_ERROR + e;

        process.env.NODE_ENV === "development" && console.trace(e);
      }

      end();

      if (errStr !== null) {
        alert(errStr);
      }
    },
    runConnect(callback) {
      if (this.connector.acquired) {
        return;
      }

      this.connector.acquired = true;
      this.connector.busy = true;

      this.getStreamThenRun(
        (stream) => {
          this.connector.busy = false;

          callback(stream);
        },
        () => {
          this.connector.busy = false;
          this.connector.acquired = false;
        },
      );
    },
    connectNew(connector) {
      const self = this;

      self.runConnect((stream) => {
        self.connector.connector = {
          id: connector.id(),
          name: connector.name(),
          description: connector.description(),
          wizard: connector.wizard(
            stream,
            self.controls,
            self.connector.historyRec,
            presets.emptyPreset(),
            null,
            false,
            () => {},
          ),
        };

        self.connector.inputting = true;
      });
    },
    connectPreset(preset) {
      const self = this;

      self.runConnect((stream) => {
        self.connector.connector = {
          id: preset.command.id(),
          name: preset.command.name(),
          description: preset.command.description(),
          wizard: preset.command.wizard(
            stream,
            self.controls,
            self.connector.historyRec,
            preset.preset,
            null,
            [],
            () => {},
          ),
        };

        self.connector.inputting = true;
      });
    },
    getConnectorByType(type) {
      let connector = null;

      for (let c in this.connector.connectors) {
        if (this.connector.connectors[c].name() !== type) {
          continue;
        }

        connector = this.connector.connectors[c];
      }

      return connector;
    },
    connectKnown(known) {
      const self = this;

      self.runConnect((stream) => {
        let connector = self.getConnectorByType(known.type);

        if (!connector) {
          alert("Unknown connector: " + known.type);

          self.connector.inputting = false;

          return;
        }

        self.connector.connector = {
          id: connector.id(),
          name: connector.name(),
          description: connector.description(),
          wizard: connector.execute(
            stream,
            self.controls,
            self.connector.historyRec,
            known.data,
            known.session,
            known.keptSessions,
            () => {
              self.connector.knowns = self.connector.historyRec.all();
            },
          ),
        };

        self.connector.inputting = true;
      });
    },
    parseConnectLauncher(ll) {
      let llSeparatorIdx = ll.indexOf(":");

      // Type must contain at least one charater
      if (llSeparatorIdx <= 0) {
        throw new Error("Invalid Launcher string");
      }

      return {
        type: ll.slice(0, llSeparatorIdx),
        query: ll.slice(llSeparatorIdx + 1, ll.length),
      };
    },
    connectLaunch(launcher, done) {
      this.showConnectWindow();

      this.runConnect((stream) => {
        let ll = this.parseConnectLauncher(launcher),
          connector = this.getConnectorByType(ll.type);

        if (!connector) {
          alert("Unknown connector: " + ll.type);

          this.connector.inputting = false;

          return;
        }

        const self = this;

        this.connector.connector = {
          id: connector.id(),
          name: connector.name(),
          description: connector.description(),
          wizard: connector.launch(
            stream,
            this.controls,
            this.connector.historyRec,
            ll.query,
            (n) => {
              self.connector.knowns = self.connector.historyRec.all();

              done(n.data().success);
            },
          ),
        };

        this.connector.inputting = true;
      });
    },
    buildknownLauncher(known) {
      let connector = this.getConnectorByType(known.type);

      if (!connector) {
        return;
      }

      return this.hostPath + "#+" + connector.launcher(known.data);
    },
    exportKnowns() {
      return this.connector.historyRec.export();
    },
    importKnowns(d) {
      this.connector.historyRec.import(d);

      this.connector.knowns = this.connector.historyRec.all();
    },
    removeKnown(uid) {
      this.connector.historyRec.del(uid);

      this.connector.knowns = this.connector.historyRec.all();
    },
    clearSessionKnown(uid) {
      this.connector.historyRec.clearSession(uid);

      this.connector.knowns = this.connector.historyRec.all();
    },
    cancelConnection() {
      this.connector.inputting = false;
      this.connector.acquired = false;
    },
    connectionSucceed(data) {
      this.connector.inputting = false;
      this.connector.acquired = false;
      this.windows.connect = false;

      this.addToTab(data);

      this.$emit("tab-opened", this.tab.tabs);

      this.startAutoReconnectSaver();
    },
    startAutoReconnectSaver() {
      if (this._autoSaveInterval) return;
      this._autoSaveInterval = setInterval(() => {
        this.saveAutoReconnect();
      }, 2000);
      // Also save after a short initial delay
      setTimeout(() => this.saveAutoReconnect(), 1000);
    },
    saveAutoReconnect() {
      try {
        const tabs = this.tab.tabs;

        // During active reconnect, never overwrite saved data — it's the
        // source-of-truth for the tabs we're still restoring
        if (this.reconnecting) {
          return;
        }

        if (tabs.length === 0) {
          sessionStorage.removeItem("sshwifty_auto_reconnect_tabs");
          sessionStorage.removeItem("sshwifty_auto_reconnect");
          return;
        }

        // Load existing saved tabs to preserve data for tabs without tokens yet
        let existingEntries = [];
        try {
          const raw = sessionStorage.getItem("sshwifty_auto_reconnect_tabs");
          if (raw) {
            existingEntries = JSON.parse(raw) || [];
          }
        } catch (_e) {
          void _e;
        }

        const knowns = this.connector.historyRec.all();
        const entries = [];

        for (let i = 0; i < tabs.length; i++) {
          const control = tabs[i].control;
          const token = control ? control.reconnectToken : null;

          if (token) {
            let userData = null;

            const tabName = tabs[i].name || "";
            const atIdx = tabName.indexOf("@");
            let tabUser = "";
            let tabHost = tabName;
            if (atIdx > 0) {
              tabUser = tabName.slice(0, atIdx);
              tabHost = tabName.slice(atIdx + 1);
            }

            for (let k = knowns.length - 1; k >= 0; k--) {
              const kn = knowns[k];
              if (!kn.data || !kn.data.user) continue;
              if (kn.data.user.startsWith("_reattach:")) continue;
              if (kn.data.host === tabHost && kn.data.user === tabUser) {
                userData = {
                  host: kn.data.host,
                  user: kn.data.user,
                  authentication: kn.data.authentication,
                  charset: kn.data.charset,
                  fingerprint: kn.data.fingerprint,
                  tabColor: kn.data.tabColor,
                };
                break;
              }
            }

            if (!userData && tabUser && tabHost) {
              userData = {
                host: tabHost,
                user: tabUser,
                authentication: "Password",
                charset: "utf-8",
                fingerprint: "",
                tabColor: "",
              };
            }

            if (!userData) {
              const existing = existingEntries.find((e) => e.token === token);
              if (existing) userData = existing.data;
            }

            if (userData) {
              entries.push({
                token: token,
                title: tabs[i].name || "",
                type: "SSH",
                data: userData,
                ts: Date.now(),
              });
            }
          } else if (i < existingEntries.length && existingEntries[i].token) {
            entries.push(existingEntries[i]);
          }
        }

        if (entries.length > 0) {
          sessionStorage.setItem(
            "sshwifty_auto_reconnect_tabs",
            JSON.stringify(entries),
          );

          const first = entries[0];
          sessionStorage.setItem(
            "sshwifty_auto_reconnect",
            JSON.stringify({
              token: first.token,
              uid: "",
              title: first.title,
              type: first.type,
              data: first.data,
              ts: Date.now(),
            }),
          );
        }
      } catch (_e) {
        // sessionStorage may be unavailable
      }
    },
    async tryAutoReconnect() {
      try {
        // Try multi-tab format first
        let entries = [];
        const rawTabs = sessionStorage.getItem("sshwifty_auto_reconnect_tabs");
        if (rawTabs) {
          const parsed = JSON.parse(rawTabs);
          entries = Array.isArray(parsed) ? parsed : [];
        } else {
          // Fall back to legacy single-tab format
          const raw = sessionStorage.getItem("sshwifty_auto_reconnect");
          if (raw) {
            const parsed = JSON.parse(raw);
            entries = parsed && typeof parsed === "object" ? [parsed] : [];
          }
        }

        if (entries.length === 0) {
          this.reconnecting = false;
          return;
        }

        const maxAge = 12 * 60 * 60 * 1000;
        const validEntries = entries.filter((saved) => {
          const elapsed = Date.now() - (saved.ts || 0);
          return elapsed <= maxAge && saved.token;
        });

        if (validEntries.length === 0) {
          this.reconnecting = false;
          return;
        }

        // Pre-warm the WebSocket connection while checking reattach API
        this.connection.get(this.socket).catch(() => {});

        let anySuccess = false;
        const survivingTokens = [];
        for (let si = 0; si < validEntries.length; si++) {
          const ok = await this.tryReattachOrReconnect(validEntries[si]);
          if (ok) {
            anySuccess = true;
            survivingTokens.push(validEntries[si]);
          }
        }

        // Remove entries whose tokens are no longer valid on the server
        if (survivingTokens.length < validEntries.length) {
          if (survivingTokens.length === 0) {
            sessionStorage.removeItem("sshwifty_auto_reconnect_tabs");
            sessionStorage.removeItem("sshwifty_auto_reconnect");
          } else {
            sessionStorage.setItem(
              "sshwifty_auto_reconnect_tabs",
              JSON.stringify(survivingTokens),
            );
          }
        }

        this.reconnecting = false;
      } catch (_e) {
        this.reconnecting = false;
        sessionStorage.removeItem("sshwifty_auto_reconnect");
        sessionStorage.removeItem("sshwifty_auto_reconnect_tabs");
      }
    },
    waitForConnectorFree() {
      return new Promise((resolve) => {
        const check = () => {
          if (!this.connector.acquired) {
            resolve();
          } else {
            setTimeout(check, 50);
          }
        };
        check();
      });
    },
    connectIndependent(known) {
      const self = this;
      return new Promise((resolve) => {
        (async () => {
          try {
            const conn = await self.connection.get(self.socket);
            const connector = self.getConnectorByType(known.type);
            if (!connector) {
              resolve(false);
              return;
            }

            const wizard = connector.execute(
              conn,
              self.controls,
              self.connector.historyRec,
              known.data,
              known.session,
              known.keptSessions,
              () => {
                self.connector.knowns = self.connector.historyRec.all();
              },
            );

            // Drive the wizard to completion without connector.vue
            // NEXT_PROMPT=1, NEXT_WAIT=2, NEXT_DONE=3
            while (true) {
              const next = await wizard.next();
              const type = next.type();
              if (type === 3) {
                const data = next.data();
                if (data.success()) {
                  self.addToTab(data.data());
                  self.$emit("tab-opened", self.tab.tabs);
                  self.startAutoReconnectSaver();
                }
                resolve(data.success());
                return;
              }
            }
          } catch (_e) {
            resolve(false);
          }
        })();
      });
    },
    async tryReattachOrReconnect(saved) {
      // Try reattach first (preserves SSH session + terminal history)
      try {
        const reattachResp = await fetch(
          "/sshwifty/session/reattach?token=" + encodeURIComponent(saved.token),
        );
        if (reattachResp.ok) {
          await reattachResp.json();
          const ok = await this.connectIndependent({
            uid: saved.uid || "",
            title: saved.title,
            type: saved.type,
            data: {
              host: saved.data.host,
              user: "_reattach:" + saved.token,
              displayUser: saved.data.user,
              authentication: "None",
              charset: saved.data.charset,
              fingerprint: saved.data.fingerprint,
              tabColor: saved.data.tabColor,
            },
            session: { credential: "" },
            keptSessions: [],
          });
          if (ok) return true;
        }
      } catch (_reattachErr) {
        void _reattachErr;
      }

      // Fall back to reconnect (new SSH session)
      try {
        const resp = await fetch(
          "/sshwifty/reconnect?token=" + encodeURIComponent(saved.token),
        );
        if (!resp.ok) return false;
        await resp.json();

        const ok = await this.connectIndependent({
          uid: saved.uid || "",
          title: saved.title,
          type: saved.type,
          data: saved.data,
          session: { credential: "_reconnect:" + saved.token },
          keptSessions: ["credential"],
        });
        return ok;
      } catch (_reconnectErr) {
        void _reconnectErr;
        return false;
      }
    },
    async addToTab(data) {
      await this.switchTab(
        this.tab.tabs.push({
          id: this.tab.lastID++,
          name: data.name,
          info: data.info,
          control: data.control,
          ui: data.ui,
          toolbar: false,
          indicator: {
            level: "",
            message: "",
            updated: false,
          },
          status: {
            closing: false,
            disconnected: false,
          },
        }) - 1,
      );
    },
    removeFromTab(index) {
      let isLast = index === this.tab.tabs.length - 1;

      this.tab.tabs.splice(index, 1);
      this.tab.current = isLast ? this.tab.tabs.length - 1 : index;
    },
    async switchTab(to) {
      if (to < 0 || to >= this.tab.tabs.length) return;

      if (this.tab.current >= 0 && this.tab.current < this.tab.tabs.length) {
        await this.tab.tabs[this.tab.current].control.disabled();
      }

      this.tab.current = to;

      this.tab.tabs[this.tab.current].indicator.updated = false;
      await this.tab.tabs[this.tab.current].control.enabled();
    },
    async retapTab(tab) {
      this.tab.tabs[tab].toolbar = !this.tab.tabs[tab].toolbar;

      await this.tab.tabs[tab].control.retap(this.tab.tabs[tab].toolbar);
    },
    async closeTab(index) {
      if (index < 0 || index >= this.tab.tabs.length) return;
      if (this.tab.tabs[index].status.closing) {
        return;
      }

      this.tab.tabs[index].status.closing = true;

      try {
        this.tab.tabs[index].control.disabled();

        await this.tab.tabs[index].control.close();
      } catch (e) {
        alert("Cannot close tab due to error: " + e);

        process.env.NODE_ENV === "development" && console.trace(e);
      }

      this.removeFromTab(index);

      this.$emit("tab-closed", this.tab.tabs);
    },
    tabStopped(index, reason) {
      if (index < 0 || index >= this.tab.tabs.length) return;
      this.tab.tabs[index].status.disconnected = true;
      if (reason !== null) {
        this.tab.tabs[index].indicator.message = "" + reason;
        this.tab.tabs[index].indicator.level = "error";
      } else {
        this.tab.tabs[index].indicator.message = "";
        this.tab.tabs[index].indicator.level = "";
      }
    },
    tabMessage(index, msg, type) {
      if (msg.toDismiss) {
        if (
          this.tab.tabs[index].indicator.message !== msg.text ||
          this.tab.tabs[index].indicator.level !== type
        ) {
          return;
        }

        this.tab.tabs[index].indicator.message = "";
        this.tab.tabs[index].indicator.level = "";

        return;
      }

      this.tab.tabs[index].indicator.message = msg.text;
      this.tab.tabs[index].indicator.level = type;
    },
    tabWarning(index, msg) {
      this.tabMessage(index, msg, "warning");
    },
    tabInfo(index, msg) {
      this.tabMessage(index, msg, "info");
    },
    tabUpdated(index) {
      this.$emit("tab-updated", this.tab.tabs);

      this.tab.tabs[index].indicator.updated = index !== this.tab.current;
    },
  },
};
</script>
