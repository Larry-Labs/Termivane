# Local browser test fixture

Run from the repository root:

```sh
go run ./tests/e2e/fixture
# Or use a specific unused loopback port:
go run ./tests/e2e/fixture -listen 127.0.0.1:22222
```

The process prints its SSH address. Connect to that address from the locally
running Sshwifty app and accept the fixture's ephemeral host fingerprint.
The SSH private key is generated in memory and never saved or printed. Restarting
the fixture changes its fingerprint and clears its in-memory files.

All inputs below are **public test data**, not deployment credentials:

| Mode | Username | Input |
|---|---|---|
| Password | `fixture-password` | `fixture-password-only` |
| Keyboard Interactive | `fixture-interactive` | First challenge: `fixture`; second challenge: `123456` |

The first challenge allows visible input; the second must use a masked input.

Shell commands:

- `session`: prints the persistent SSH channel ID and requested terminal type.
  Expect `TERM=xterm-256color`; the ID must stay the same after browser refresh.
- `unicode`: emits `中文跨块测试🙂完成` one byte at a time to exercise streaming decoding.
- `osc52`: asks to copy `fixture clipboard text` into the browser clipboard.
  Test deny, allow once, and allow for session.
- `burst`: prints `BURST_00000` through `BURST_09999`, then `BURST_COMPLETE`.
- `echo TEXT`: echoes a test marker; `exit` ends this fixture shell.

Open the app's SFTP panel and create/upload/download/rename/delete test files.
The filesystem is entirely in memory and shared between fixture connections,
so password-mode independent SFTP connections see the same files. Interactive
mode can open an SFTP channel over its authenticated SSH transport without
repeating the two challenges. Paths such as `/etc/passwd` do not access host
files. The fixture never starts a local shell or executes user commands.

Stop with Ctrl-C. Validate the fixture itself with:

```sh
go test -race ./tests/e2e/fixture -timeout 30s
```
