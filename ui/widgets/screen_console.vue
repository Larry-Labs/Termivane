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
  <div class="screen-console">
    <div class="console-main-area">
      <div
        v-if="filePanel"
        class="file-panel"
        :style="{
          width: filePanelWidth + 'px',
          minWidth: filePanelWidth + 'px',
          maxWidth: filePanelWidth + 'px',
        }"
      >
        <div class="file-panel-topbar">
          <div class="file-panel-breadcrumb">
            <a
              v-for="(seg, idx) in pathSegments"
              :key="idx"
              href="javascript:;"
              class="breadcrumb-item"
              :class="{ 'breadcrumb-root': idx === 0 }"
              @click="navigateToSegment(idx)"
              >{{ seg }}</a
            >
          </div>
          <div class="file-panel-actions">
            <a
              class="fp-btn"
              href="javascript:;"
              title="Refresh"
              @click="refreshFiles"
              >↻</a
            >
            <a
              class="fp-btn"
              href="javascript:;"
              title="Upload"
              @click="triggerUpload"
              >↑</a
            >
            <a
              class="fp-btn"
              href="javascript:;"
              title="New Folder"
              @click="createDirectory"
              >+</a
            >
            <a
              class="fp-btn fp-btn-close"
              href="javascript:;"
              title="Close"
              @click="toggleFilePanel"
              >✕</a
            >
          </div>
          <input
            ref="fileUploadInput"
            type="file"
            multiple
            style="display: none"
            @change="handleFileSelect"
          />
        </div>

        <div v-if="fileLoading" class="file-panel-loading">Loading...</div>
        <div v-if="fileError" class="file-panel-error">{{ fileError }}</div>

        <div class="file-list">
          <div
            v-if="currentPath !== '/'"
            class="file-item file-item-dir"
            @click="navigateUp"
          >
            <span class="file-icon file-icon-dir">&laquo;</span>
            <span class="file-name">..</span>
          </div>
          <div
            v-for="f in sortedFiles"
            :key="f.name"
            class="file-item"
            :class="{ 'file-item-dir': f.isDir }"
            @click="fileItemClick(f)"
          >
            <span
              class="file-icon"
              :class="f.isDir ? 'file-icon-dir' : 'file-icon-file'"
              >{{ f.isDir ? "/" : " " }}</span
            >
            <span class="file-name" :title="f.name">{{ f.name }}</span>
            <span class="file-size">{{
              f.isDir ? "" : formatSize(f.size)
            }}</span>
            <a
              v-if="!f.isDir"
              class="file-action"
              href="javascript:;"
              title="Download"
              @click.stop="downloadFile(f)"
              >Save</a
            >
            <a
              class="file-action file-action-delete"
              href="javascript:;"
              title="Delete"
              @click.stop="deleteItem(f)"
              >Del</a
            >
          </div>
        </div>

        <div v-if="transfers.length > 0" class="file-transfers">
          <div class="transfer-title">Transfers ({{ transfers.length }})</div>
          <div v-for="(t, idx) in transfers" :key="idx" class="transfer-item">
            <div class="transfer-info">
              <span class="transfer-name" :title="t.name">{{ t.name }}</span>
              <span
                v-if="t.status === 'queued'"
                class="transfer-status transfer-queued"
                >queued</span
              >
              <span
                v-else-if="t.progress < 0"
                class="transfer-status transfer-failed"
                >failed</span
              >
              <span v-else class="transfer-status transfer-active">
                {{ t.progress }}%
                <span v-if="t.speed > 0" class="transfer-speed">{{
                  formatSpeed(t.speed)
                }}</span>
              </span>
            </div>
            <div
              v-if="t.status !== 'queued' && t.progress >= 0"
              class="transfer-bar-bg"
            >
              <div
                class="transfer-bar-fill"
                :style="{ width: t.progress + '%' }"
              ></div>
            </div>
          </div>
        </div>
      </div>

      <div class="file-panel-resize" @mousedown="startResize"></div>

      <div
        class="console-console-wrapper"
        @dragover.prevent="onDragOver"
        @dragleave.prevent="onDragLeave"
        @drop.prevent="onDrop"
      >
        <div
          v-if="
            clipboardState.pending ||
            clipboardState.allowed ||
            clipboardState.error
          "
          class="console-clipboard"
          role="status"
        >
          <template v-if="clipboardState.pending">
            <span
              >Remote terminal requests permission to copy to your
              clipboard.</span
            >
            <button type="button" @click="term.clipboard.reject()">
              Don't copy
            </button>
            <button type="button" @click="term.clipboard.allow()">
              Copy once
            </button>
            <button type="button" @click="term.clipboard.allow(true)">
              Allow for this session
            </button>
          </template>
          <template v-else-if="clipboardState.allowed">
            <span>Remote clipboard copies are allowed for this terminal.</span>
            <button type="button" @click="term.clipboard.revoke()">
              Stop allowing
            </button>
          </template>
          <template v-else>
            <span>{{ clipboardState.error }}</span>
            <button type="button" @click="term.clipboard.reject()">
              Dismiss
            </button>
          </template>
        </div>
        <div
          v-if="toolbar"
          class="console-toolbar"
          :style="'background-color: ' + control.color() + 'ee'"
        >
          <h2 style="display: none">Tool bar</h2>

          <div class="console-toolbar-group console-toolbar-group-left">
            <div class="console-toolbar-item">
              <h3 class="tb-title">Text size</h3>

              <ul class="lst-nostyle">
                <li>
                  <a class="tb-item" href="javascript:;" @click="fontSizeUp">
                    <span
                      class="tb-key-icon tb-key-resize-icon icon icon-keyboardkey1 icon-iconed-bottom1"
                    >
                      <i>+</i>
                      Increase
                    </span>
                  </a>
                </li>
                <li>
                  <a class="tb-item" href="javascript:;" @click="fontSizeDown">
                    <span
                      class="tb-key-icon tb-key-resize-icon icon icon-keyboardkey1 icon-iconed-bottom1"
                    >
                      <i>-</i>
                      Decrease
                    </span>
                  </a>
                </li>
              </ul>
            </div>

            <div v-if="hasSFTP" class="console-toolbar-item">
              <h3 class="tb-title">Files</h3>
              <ul class="lst-nostyle">
                <li>
                  <a
                    class="tb-item"
                    href="javascript:;"
                    @click="toggleFilePanel"
                  >
                    <span
                      class="tb-key-icon tb-key-resize-icon icon icon-keyboardkey1 icon-iconed-bottom1"
                    >
                      {{ filePanel ? "Hide" : "Show" }}
                    </span>
                  </a>
                </li>
              </ul>
            </div>
          </div>

          <div class="console-toolbar-group console-toolbar-group-main">
            <div
              v-for="(keyType, keyTypeIdx) in screenKeys"
              :key="keyTypeIdx"
              class="console-toolbar-item"
            >
              <h3 class="tb-title">{{ keyType.title }}</h3>

              <ul class="hlst lst-nostyle">
                <li v-for="(key, keyIdx) in keyType.keys" :key="keyIdx">
                  <a
                    class="tb-item"
                    href="javascript:;"
                    @click="sendSpecialKey(key[1])"
                    v-html="$options.filters.specialKeyHTML(key[0])"
                  ></a>
                </li>
              </ul>
            </div>
          </div>
        </div>

        <div v-if="dragOver" class="drop-overlay">
          <div class="drop-overlay-text">Drop files to upload</div>
        </div>

        <div
          class="console-console"
          :style="'font-family: ' + typefaces + ', inherit'"
        >
          <h2 style="display: none">Console</h2>

          <div class="console-loading">
            <div class="console-loading-frame">
              <div class="console-loading-icon"></div>
              <div class="console-loading-message">
                Initializing console ...
              </div>
            </div>
          </div>
        </div>

        <a
          v-if="hasSFTP && !filePanel"
          class="sftp-pill"
          href="javascript:;"
          @click="toggleFilePanel"
          >SFTP</a
        >
      </div>
    </div>
  </div>
</template>

<script>
import FontFaceObserver from "fontfaceobserver";
import { Terminal } from "@xterm/xterm";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { WebglAddon } from "@xterm/addon-webgl";
import { FitAddon } from "@xterm/addon-fit";
import { ClipboardAddon, Base64 } from "@xterm/addon-clipboard";
import { ClipboardProvider } from "./clipboard.js";
import { isNumber } from "../commands/common.js";
import { consoleScreenKeys } from "./screen_console_keys.js";

import "./screen_console.css";
import "./clipboard.css";
import "@xterm/xterm/css/xterm.css";

const termTypeFaces = "Hack, PureNerdFont";
const termFallbackTypeFace = '"Cascadia Code" , monospace';
const termTypeFaceLoadTimeout = 3000;
const termTypeFaceLoadError =
  "Remote font " +
  termTypeFaces +
  " is unavailable, using " +
  termFallbackTypeFace +
  " instead until the remote font is loaded";
const termDefaultFontSize = 16;
const termMinFontSize = 8;
const termMaxFontSize = 36;

function webglSupported() {
  try {
    if (typeof window !== "object") {
      return false;
    }
    if (typeof window.WebGLRenderingContext !== "function") {
      return false;
    }
    if (typeof window.WebGL2RenderingContext !== "function") {
      return false;
    }
    return (
      document.createElement("canvas").getContext("webgl") &&
      document.createElement("canvas").getContext("webgl2")
    );
  } catch {
    // Canvas creation may be blocked; use the terminal's default renderer.
  }
  return false;
}

class Term {
  constructor(control, clipboardChanged) {
    const resizeDelayInterval = 500;

    this.control = control;
    this.closed = false;
    this.clipboard = new ClipboardProvider(clipboardChanged);
    this.fontSize = this._loadFontSize();
    this.term = new Terminal({
      allowProposedApi: true,
      allowTransparency: false,
      cursorBlink: true,
      cursorStyle: "block",
      fontFamily: termTypeFaces + ", " + termFallbackTypeFace,
      fontSize: this.fontSize,
      letterSpacing: 1,
      lineHeight: 1.3,
      scrollback: 999999,
      logLevel: process.env.NODE_ENV === "development" ? "info" : "off",
      theme: {
        background: this.control.color(),
      },
    });
    this.fit = new FitAddon();

    this.term.onData((data) => {
      if (this.closed) {
        return;
      }
      this.control.send(data);
    });

    this.term.onBinary((data) => {
      if (this.closed) {
        return;
      }
      this.control.sendBinary(data);
    });

    this.term.onKey((ev) => {
      if (this.closed) {
        return;
      }
      if (!this.control.echo()) {
        return;
      }
      const printable =
        !ev.domEvent.altKey &&
        !ev.domEvent.altGraphKey &&
        !ev.domEvent.ctrlKey &&
        !ev.domEvent.metaKey;
      switch (ev.domEvent.key) {
        case "Enter":
          ev.domEvent.preventDefault();
          this.writeStr("\r\n");
          break;
        case "Backspace":
          ev.domEvent.preventDefault();
          this.writeStr("\b \b");
          break;
        default:
          if (printable) {
            ev.domEvent.preventDefault();
            this.writeStr(ev.key);
          }
      }
    });

    let resizeDelay = null,
      oldRows = 0,
      oldCols = 0;

    this.term.onResize((dim) => {
      if (this.closed) {
        return;
      }
      if (dim.cols === oldCols && dim.rows === oldRows) {
        return;
      }
      oldRows = dim.rows;
      oldCols = dim.cols;
      if (resizeDelay !== null) {
        clearTimeout(resizeDelay);
        resizeDelay = null;
      }
      resizeDelay = setTimeout(() => {
        resizeDelay = null;
        if (!isNumber(dim.cols) || !isNumber(dim.rows)) {
          return;
        }
        if (!dim.cols || !dim.rows) {
          return;
        }
        this.control.resize({
          rows: dim.rows,
          cols: dim.cols,
        });
      }, resizeDelayInterval);
    });
  }

  init(root) {
    if (this.closed) {
      return;
    }
    this.term.open(root);
    this.term.loadAddon(this.fit);
    this.term.loadAddon(new WebLinksAddon());
    this.term.loadAddon(new Unicode11Addon());
    this.term.loadAddon(new ClipboardAddon(new Base64(), this.clipboard));
    try {
      if (webglSupported()) {
        this.term.loadAddon(new WebglAddon());
      }
    } catch (e) {
      process.env.NODE_ENV === "development" && console.debug(e);
    }
    this.term.unicode.activeVersion = "11";
    this.refit();
  }

  dispatch(event) {
    if (this.closed) {
      return;
    }
    try {
      this.term.textarea.dispatchEvent(event);
    } catch (e) {
      process.env.NODE_ENV === "development" && console.trace(e);
    }
  }

  writeStr(d) {
    if (this.closed) {
      return;
    }
    try {
      this.term.write(d);
    } catch (e) {
      process.env.NODE_ENV === "development" && console.trace(e);
    }
  }

  setFont(value) {
    if (this.closed) {
      return;
    }
    this.term.options.fontFamily = value;
    this.refit();
  }

  fontSizeUp() {
    if (this.closed) {
      return;
    }
    if (this.fontSize >= termMaxFontSize) {
      return;
    }
    this.fontSize += 2;
    this.term.options.fontSize = this.fontSize;
    this._saveFontSize();
    this.refit();
  }

  fontSizeDown() {
    if (this.closed) {
      return;
    }
    if (this.fontSize <= termMinFontSize) {
      return;
    }
    this.fontSize -= 2;
    this.term.options.fontSize = this.fontSize;
    this._saveFontSize();
    this.refit();
  }

  _saveFontSize() {
    try {
      localStorage.setItem("sshwifty_font_size", String(this.fontSize));
    } catch (_e) {
      /* localStorage unavailable */
    }
  }

  _loadFontSize() {
    try {
      const saved = localStorage.getItem("sshwifty_font_size");
      if (saved) {
        const size = parseInt(saved, 10);
        if (size >= termMinFontSize && size <= termMaxFontSize) {
          return size;
        }
      }
    } catch (_e) {
      /* localStorage unavailable */
    }
    return termDefaultFontSize;
  }

  focus() {
    if (this.closed) {
      return;
    }
    try {
      this.term.focus();
      this.refit();
    } catch (e) {
      process.env.NODE_ENV === "development" && console.trace(e);
    }
  }

  blur() {
    if (this.closed) {
      return;
    }
    try {
      this.term.blur();
    } catch (e) {
      process.env.NODE_ENV === "development" && console.trace(e);
    }
  }

  refit() {
    if (this.closed) {
      return;
    }
    try {
      this.fit.fit();
    } catch (e) {
      process.env.NODE_ENV === "development" && console.trace(e);
    }
  }

  destroyed() {
    return this.closed;
  }

  destroy() {
    if (this.closed) {
      return;
    }
    this.closed = true;
    this.clipboard.dispose();
    try {
      this.term.dispose();
    } catch (e) {
      process.env.NODE_ENV === "development" && console.trace(e);
    }
  }
}

// So it turns out, display: none + xterm.js == trouble, so I changed this
// to a visibility + position: absolute appoarch. Problem resolved, and I
// like to keep it that way.

export default {
  filters: {
    specialKeyHTML(key) {
      const head = '<span class="tb-key-icon icon icon-keyboardkey1">',
        tail = "</span>";

      return head + key.split("+").join(tail + "+" + head) + tail;
    },
  },
  props: {
    active: {
      type: Boolean,
      default: false,
    },
    control: {
      type: Object,
      default: () => null,
    },
    change: {
      type: Object,
      default: () => null,
    },
    toolbar: {
      type: Boolean,
      default: false,
    },
    viewPort: {
      type: Object,
      default: () => null,
    },
  },
  data() {
    return {
      screenKeys: consoleScreenKeys,
      term: new Term(this.control, (state) => {
        this.clipboardState = state;
        this.$nextTick(() => this.fit());
      }),
      clipboardState: { pending: false, allowed: false, error: "" },
      typefaces: termTypeFaces,
      runner: null,
      eventHandlers: {
        keydown: null,
        keyup: null,
      },
      hasSFTP: false,
      filePanel: false,
      filePanelWidth: 260,
      resizing: false,
      currentPath: "/",
      files: [],
      fileLoading: false,
      fileError: "",
      dragOver: false,
      transfers: [],
    };
  },
  computed: {
    pathSegments() {
      if (this.currentPath === "/") return ["/"];
      const parts = this.currentPath.split("/").filter((s) => s.length > 0);
      return ["/"].concat(parts);
    },
    sortedFiles() {
      const dirs = this.files
        .filter((f) => f.isDir)
        .sort((a, b) => a.name.localeCompare(b.name));
      const regular = this.files
        .filter((f) => !f.isDir)
        .sort((a, b) => a.name.localeCompare(b.name));
      return dirs.concat(regular);
    },
  },
  watch: {
    active(newVal) {
      this.triggerActive(newVal);
    },
    change: {
      handler() {
        if (!this.active) {
          return;
        }

        this.fit();
      },
      deep: true,
    },
    viewPort: {
      handler() {
        if (!this.active) {
          return;
        }

        this.fit();
      },
      deep: true,
    },
    filePanel() {
      const self = this;
      setTimeout(() => {
        self.fit();
      }, 50);
    },
    hasSFTP() {
      const self = this;
      this.$nextTick(() => {
        setTimeout(() => {
          self.fit();
        }, 50);
      });
    },
  },
  async mounted() {
    await this.init();
  },
  beforeDestroy() {
    this.deinit();
  },
  methods: {
    loadRemoteFont(typefaces, timeout) {
      const tfs = typefaces.split(",");
      let observers = [];
      for (let v in tfs) {
        observers.push(new FontFaceObserver(tfs[v].trim()).load(null, timeout));
        observers.push(
          new FontFaceObserver(tfs[v].trim(), {
            weight: "bold",
          }).load(null, timeout),
        );
      }
      return Promise.all(observers);
    },
    async retryLoadRemoteFont(typefaces, timeout, onSuccess) {
      const self = this;
      for (;;) {
        try {
          onSuccess(await self.loadRemoteFont(typefaces, timeout));
          return;
        } catch (e) {
          await new Promise((res) => {
            window.setTimeout(() => {
              res();
            }, timeout);
          });
        }
      }
    },
    async openTerm(root, callbacks) {
      const self = this;
      try {
        await self.loadRemoteFont(termTypeFaces, termTypeFaceLoadTimeout);
        if (self.term.destroyed()) {
          return;
        }
        root.innerHTML = "";
        self.term.init(root);
        return;
      } catch (e) {
        // Ignore
      }
      if (self.term.destroyed()) {
        return;
      }
      root.innerHTML = "";
      callbacks.warn(termTypeFaceLoadError, false);
      self.term.setFont(termFallbackTypeFace);
      self.term.init(root);
      self.retryLoadRemoteFont(termTypeFaces, termTypeFaceLoadTimeout, () => {
        if (self.term.destroyed()) {
          return;
        }
        self.term.setFont(termTypeFaces);
        callbacks.warn(termTypeFaceLoadError, true);
      });
    },
    triggerActive(active) {
      active ? this.activate() : this.deactivate();
    },
    async init() {
      let self = this;

      await self.openTerm(
        self.$el.getElementsByClassName("console-console")[0],
        {
          warn(msg, toDismiss) {
            self.$emit("warning", {
              text: msg,
              toDismiss: toDismiss,
            });
          },
          info(msg, toDismiss) {
            self.$emit("info", {
              text: msg,
              toDismiss: toDismiss,
            });
          },
        },
      );

      if (self.term.destroyed()) {
        return;
      }

      self.triggerActive(this.active);
      self.runRunner();
      self.initSFTPListener();
    },
    initSFTPListener() {
      if (!this.control) return;
      const self = this;
      this.control.onToggleFiles(() => {
        self.toggleFilePanel();
      });

      const waitForSession = () => {
        if (self.term.destroyed()) return;
        if (self.control && self.control.sessionID) {
          self.hasSFTP = true;
        } else {
          setTimeout(waitForSession, 500);
        }
      };
      waitForSession();
    },
    async deinit() {
      if (this._resizeCleanup) {
        this._resizeCleanup();
        this._resizeCleanup = null;
      }
      await this.closeRunner();
      await this.deactivate();
      this.term.destroy();
    },
    fit() {
      this.term.refit();
    },
    activate() {
      this.term.focus();
      this.fit();
    },
    async deactivate() {
      this.term.blur();
    },
    runRunner() {
      if (this.runner !== null) {
        return;
      }
      let self = this;
      this.runner = (async () => {
        try {
          for (;;) {
            if (self.term.destroyed()) {
              break;
            }
            self.term.writeStr(await this.control.receive());
            self.$emit("updated");
          }
        } catch (e) {
          self.$emit("stopped", e);
        }
      })();
    },
    async closeRunner() {
      if (this.runner === null) {
        return;
      }

      let runner = this.runner;
      this.runner = null;

      await runner;
    },
    sendSpecialKey(key) {
      if (!this.term) {
        return;
      }

      this.term.dispatch(new KeyboardEvent("keydown", key));
      this.term.dispatch(new KeyboardEvent("keyup", key));
    },
    fontSizeUp() {
      this.term.fontSizeUp();
    },
    fontSizeDown() {
      this.term.fontSizeDown();
    },
    toggleFilePanel() {
      this.filePanel = !this.filePanel;
      if (this.filePanel && this.files.length === 0) {
        this.refreshFiles();
      }
      this.$nextTick(() => {
        this.refit();
      });
    },
    startResize(e) {
      e.preventDefault();
      this.resizing = true;
      const startX = e.clientX;
      const startWidth = this.filePanelWidth;
      const onMouseMove = (ev) => {
        const delta = ev.clientX - startX;
        const newWidth = Math.min(Math.max(startWidth + delta, 160), 500);
        this.filePanelWidth = newWidth;
      };
      const onMouseUp = () => {
        this.resizing = false;
        this._resizeCleanup = null;
        document.removeEventListener("mousemove", onMouseMove);
        document.removeEventListener("mouseup", onMouseUp);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      this._resizeCleanup = () => {
        document.removeEventListener("mousemove", onMouseMove);
        document.removeEventListener("mouseup", onMouseUp);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      document.addEventListener("mousemove", onMouseMove);
      document.addEventListener("mouseup", onMouseUp);
    },
    async refreshFiles() {
      if (!this.control || !this.control.sessionID) return;
      this.fileLoading = true;
      this.fileError = "";
      try {
        const client = await this.control.getSFTPClient();
        this.files = await client.list(this.currentPath);
        this.fileLoading = false;
      } catch (e) {
        this.fileLoading = false;
        this.fileError = e.message || "Request failed";
      }
    },
    navigateToSegment(idx) {
      if (idx === 0) {
        this.currentPath = "/";
      } else {
        const parts = this.currentPath.split("/").filter((s) => s.length > 0);
        this.currentPath = "/" + parts.slice(0, idx).join("/");
      }
      this.refreshFiles();
    },
    navigateUp() {
      const parts = this.currentPath.split("/").filter((s) => s.length > 0);
      parts.pop();
      this.currentPath = parts.length > 0 ? "/" + parts.join("/") : "/";
      this.refreshFiles();
    },
    fileItemClick(f) {
      if (f.isDir) {
        const newPath =
          this.currentPath === "/"
            ? "/" + f.name
            : this.currentPath + "/" + f.name;
        this.currentPath = newPath;
        this.refreshFiles();
      }
    },
    async downloadFile(f) {
      if (!this.control || !this.control.sessionID) return;
      const filePath =
        this.currentPath === "/"
          ? "/" + f.name
          : this.currentPath + "/" + f.name;
      try {
        const client = await this.control.getSFTPClient();
        const blob = await client.download(filePath);
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = f.name;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
      } catch (e) {
        this.fileError = e.message || "Download failed";
      }
    },
    async deleteItem(f) {
      const msg = f.isDir
        ? 'Delete folder "' + f.name + '" and ALL its contents?'
        : 'Delete file "' + f.name + '"?';
      if (!confirm(msg)) return;
      const itemPath =
        this.currentPath === "/"
          ? "/" + f.name
          : this.currentPath + "/" + f.name;
      this.fileLoading = true;
      try {
        const client = await this.control.getSFTPClient();
        await client.delete(itemPath);
        await this.refreshFiles();
      } catch (e) {
        this.fileLoading = false;
        this.fileError = e.message || "Delete failed";
      }
    },
    async createDirectory() {
      const name = prompt("New directory name:");
      if (!name) return;
      const dirPath =
        this.currentPath === "/" ? "/" + name : this.currentPath + "/" + name;
      this.fileLoading = true;
      try {
        const client = await this.control.getSFTPClient();
        await client.mkdir(dirPath);
        await this.refreshFiles();
      } catch (e) {
        this.fileLoading = false;
        this.fileError = e.message || "Mkdir failed";
      }
    },
    triggerUpload() {
      this.$refs.fileUploadInput.click();
    },
    async handleFileSelect(e) {
      const fileList = e.target.files;
      if (!fileList || fileList.length === 0) return;
      const files = Array.from(fileList);
      e.target.value = "";
      this.enqueueUploads(files);
    },
    enqueueUploads(files) {
      if (!this.control || !this.control.sessionID) return;
      const tasks = files.map((file) => ({
        file,
        transfer: {
          name: file.name,
          progress: 0,
          status: "queued",
          size: file.size,
        },
      }));
      for (const t of tasks) {
        this.transfers.push(t.transfer);
      }
      this.processUploadQueue(tasks);
    },
    async processUploadQueue(tasks) {
      for (const task of tasks) {
        await this.uploadSingleFile(task.file, task.transfer);
      }
    },
    async uploadSingleFile(file, transfer) {
      if (!this.control || !this.control.sessionID) return;
      const filePath =
        this.currentPath === "/"
          ? "/" + file.name
          : this.currentPath + "/" + file.name;

      try {
        const client = await this.control.getSFTPClient();
        let started = false;
        await client.upload(filePath, file, (pct, speed) => {
          if (!started) {
            started = true;
            this.$set(transfer, "status", "uploading");
          }
          this.$set(transfer, "progress", pct);
          if (speed > 0) {
            this.$set(transfer, "speed", speed);
          }
        });

        const idx = this.transfers.indexOf(transfer);
        if (idx >= 0) this.transfers.splice(idx, 1);

        await this.refreshFiles();
      } catch (e) {
        this.$set(transfer, "progress", -1);
        this.$set(transfer, "status", "failed");
        this.fileError = "Upload failed: " + (e.message || e);
      }
    },
    formatSize(bytes) {
      if (bytes < 1024) return bytes + " B";
      if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB";
      if (bytes < 1024 * 1024 * 1024)
        return (bytes / (1024 * 1024)).toFixed(1) + " MB";
      return (bytes / (1024 * 1024 * 1024)).toFixed(1) + " GB";
    },
    formatSpeed(bytesPerSec) {
      if (bytesPerSec < 1024) return bytesPerSec.toFixed(0) + " B/s";
      if (bytesPerSec < 1024 * 1024)
        return (bytesPerSec / 1024).toFixed(1) + " KB/s";
      if (bytesPerSec < 1024 * 1024 * 1024)
        return (bytesPerSec / (1024 * 1024)).toFixed(1) + " MB/s";
      return (bytesPerSec / (1024 * 1024 * 1024)).toFixed(1) + " GB/s";
    },
    onDragOver() {
      if (this.hasSFTP) this.dragOver = true;
    },
    onDragLeave() {
      this.dragOver = false;
    },
    onDrop(e) {
      this.dragOver = false;
      if (!this.hasSFTP) return;
      const droppedFiles = e.dataTransfer.files;
      if (!droppedFiles || droppedFiles.length === 0) return;
      if (!this.filePanel) {
        this.filePanel = true;
        if (this.files.length === 0) this.refreshFiles();
      }
      this.enqueueUploads(Array.from(droppedFiles));
    },
  },
};
</script>
