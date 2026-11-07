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
  <window
    id="connect"
    flash-class="home-window-display"
    :display="display"
    @display="$emit('display', $event)"
  >
    <div id="connect-frame">
      <slot v-if="inputting"></slot>

      <template v-if="!inputting">
        <h1 class="window-title">SSH Connection</h1>

        <div v-if="singleConnector" id="connect-new-btn-wrap">
          <a id="connect-new-btn" href="javascript:;" @click="autoSelectSingle"
            >+ New SSH Connection</a
          >
        </div>

        <connect-new
          v-if="!singleConnector"
          :connectors="connectors"
          @select="selectConnector"
        ></connect-new>

        <connect-known
          :presets="presets"
          :restricted-to-presets="restrictedToPresets"
          :knowns="knowns"
          :launcher-builder="knownsLauncherBuilder"
          :knowns-export="knownsExport"
          :knowns-import="knownsImport"
          @select="selectKnown"
          @select-preset="selectPreset"
          @remove="removeKnown"
          @clear-session="clearSessionKnown"
        ></connect-known>
      </template>

      <div id="connect-warning">
        <span id="connect-warning-icon" class="icon icon-warning1"></span>
        <div id="connect-warning-msg">
          <p>
            <strong>An insecured service may steal your secrets.</strong>
            Always exam the safety of the service before using it.
          </p>

          <p>
            Termivane is free software, you can deploy it on your own trusted
            infrastructure.
            <a href="https://github.com/Larry-Labs/sshwifty" target="_blank"
              >Learn more</a
            >
          </p>
        </div>
      </div>

      <div v-if="busy" id="connect-busy-overlay"></div>
    </div>
  </window>
</template>

<script>
import "./connect.css";

import Window from "./window.vue";
import ConnectKnown from "./connect_known.vue";
import ConnectNew from "./connect_new.vue";

export default {
  components: {
    window: Window,
    "connect-known": ConnectKnown,
    "connect-new": ConnectNew,
  },
  props: {
    display: {
      type: Boolean,
      default: false,
    },
    inputting: {
      type: Boolean,
      default: false,
    },
    presets: {
      type: Array,
      default: () => [],
    },
    restrictedToPresets: {
      type: Boolean,
      default: () => false,
    },
    knowns: {
      type: Array,
      default: () => [],
    },
    knownsLauncherBuilder: {
      type: Function,
      default: () => [],
    },
    knownsExport: {
      type: Function,
      default: () => [],
    },
    knownsImport: {
      type: Function,
      default: () => [],
    },
    connectors: {
      type: Array,
      default: () => [],
    },
    busy: {
      type: Boolean,
      default: false,
    },
  },
  computed: {
    singleConnector() {
      return this.connectors.length === 1;
    },
  },
  watch: {
    display(visible) {
      if (
        visible &&
        !this.inputting &&
        this.singleConnector &&
        this.knowns.length === 0
      ) {
        this.$nextTick(() => {
          this.autoSelectSingle();
        });
      }
    },
  },
  methods: {
    autoSelectSingle() {
      this.selectConnector(this.connectors[0]);
    },
    selectConnector(connector) {
      if (this.inputting) {
        return;
      }

      this.$emit("connector-select", connector);
    },
    selectKnown(known) {
      if (this.inputting) {
        return;
      }

      this.$emit("known-select", known);
    },
    removeKnown(uid) {
      if (this.inputting) {
        return;
      }

      this.$emit("known-remove", uid);
    },
    selectPreset(preset) {
      if (this.inputting) {
        return;
      }

      this.$emit("preset-select", preset);
    },
    clearSessionKnown(uid) {
      if (this.inputting) {
        return;
      }

      this.$emit("known-clear-session", uid);
    },
  },
};
</script>
