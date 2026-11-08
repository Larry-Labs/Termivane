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

import { IconvDecoder } from "../iconv/decoder.js";
import { IconvEncoder } from "../iconv/encoder.js";
import * as color from "../commands/color.js";
import * as common from "../commands/common.js";
import * as reader from "../stream/reader.js";
import * as subscribe from "../stream/subscribe.js";
import { SFTPClient } from "../sftp/client.js";

class Control {
  constructor(data, color) {
    this.background = color;
    this.charset = data.charset;

    this.closed = false;
    this.enable = false;
    this.sender = data.send;
    this.closer = data.close;
    this.resizer = data.resize;
    this.subs = new subscribe.Subscribe();
    this._toggleFilesHandler = null;
    this.sftpClient = null;
    this.sessionID = null;
    this.reconnectToken = null;

    let self = this;
    this.charsetEncoder = new IconvEncoder(this.sender, this.charset);
    // stdout and stderr are independent byte streams: do not join partial
    // characters from one stream with bytes from the other.
    const stdoutDecoder = new IconvDecoder(
      (s) => self.subs.resolve(s),
      this.charset,
    );
    const stderrDecoder = new IconvDecoder(
      (s) => self.subs.resolve(s),
      this.charset,
    );

    data.events.place("stdout", async (rd) => {
      try {
        stdoutDecoder.write(await reader.readCompletely(rd));
      } catch (e) {
        // Do nothing
      }
    });

    data.events.place("stderr", async (rd) => {
      try {
        stderrDecoder.write(await reader.readCompletely(rd));
      } catch (e) {
        // Do nothing
      }
    });

    data.events.place("session_id", async (rd) => {
      try {
        const raw = await reader.readCompletely(rd);
        const text = new TextDecoder().decode(raw);
        const parts = text.split("\n");
        self.sessionID = parts[0];
        if (parts.length > 1) {
          self.reconnectToken = parts[1];
        }
      } catch (e) {
        // Do nothing
      }
    });

    data.events.place("completed", () => {
      self.closed = true;
      stdoutDecoder.close();
      stderrDecoder.close();
      self.background.forget();
      if (self.sftpClient) {
        self.sftpClient.close();
        self.sftpClient = null;
      }
      self.subs.reject("Remote connection has been terminated");
    });
  }

  echo() {
    return false;
  }

  resize(dim) {
    if (this.closed) {
      return;
    }

    this.resizer(dim.rows, dim.cols);
  }

  enabled() {
    this.enable = true;
  }

  disabled() {
    this.enable = false;
  }

  retap(isOn) {}

  receive() {
    return this.subs.subscribe();
  }

  send(data) {
    if (this.closed) {
      return;
    }

    return this.charsetEncoder.write(data);
  }

  sendBinary(data) {
    if (this.closed) {
      return;
    }

    return this.sender(common.strToBinary(data));
  }

  color() {
    return this.background.hex();
  }

  hasSFTP() {
    return this.sessionID !== null;
  }

  async getSFTPClient() {
    if (this.sftpClient && this.sftpClient.isConnected()) {
      return this.sftpClient;
    }
    if (!this.sessionID) {
      throw new Error("No session ID available");
    }
    if (!this.sftpClient) this.sftpClient = new SFTPClient();
    await this.sftpClient.connect(this.sessionID);
    return this.sftpClient;
  }

  onToggleFiles(handler) {
    this._toggleFilesHandler = handler;
  }

  toggleFiles() {
    if (this._toggleFilesHandler) {
      this._toggleFilesHandler();
    }
  }

  close() {
    if (this.sftpClient) {
      this.sftpClient.close();
      this.sftpClient = null;
    }

    if (this.closer === null) {
      return;
    }

    let cc = this.closer;
    this.closer = null;

    return cc();
  }
}

export class SSH {
  /**
   * constructor
   *
   * @param {color.Colors} c
   */
  constructor(c) {
    this.colors = c;
  }

  type() {
    return "SSH";
  }

  ui() {
    return "Console";
  }

  build(data) {
    return new Control(data, this.colors.get(data.tabColor));
  }
}
