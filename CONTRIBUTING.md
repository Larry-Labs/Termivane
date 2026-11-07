# Contributing

Termivane extends [nirui/sshwifty](https://github.com/nirui/sshwifty) with persistent SSH sessions, SFTP, and terminal interface changes. Keep those behaviors intact when adapting upstream work.

## Set up a development checkout

Use Go 1.27.3+, Node.js 22+, npm, and Bash. Go race tests also require CGO and a supported C toolchain.

```bash
npm ci
npm run build
npm run lint
npm run testonly
```

`npm run dev` watches frontend assets. Run or rebuild the Go application separately when needed. Build output lives in `.tmp/`, generated static Go files, and the `sshwifty` executable; do not include these in a source-only pull request.

## Keep changes focused

- Describe the user-visible problem and the resulting behavior.
- Preserve compatibility with the fork's SSH command IDs, session recovery, credential settings, and SFTP transport choices.
- Adapt upstream changes to this codebase; do not replace customized modules without checking their callers.
- Add regression coverage for behavior changes. Do not disable existing tests or checks to make a change pass.
- Explain new dependencies and commit the matching lockfile changes.

For terminal, authentication, or SFTP changes, use the [local browser fixture](tests/e2e/README.md). Check the relevant password/private-key/interactive authentication path, refresh recovery, file transfers, and desktop/mobile UI behavior.

## Describe a bug

Include:

1. The commit or build used, browser, operating system, and deployment method.
2. Steps to reproduce, expected behavior, and actual behavior.
3. Whether the problem involves a fresh connection, a refreshed tab, or SFTP.
4. Relevant sanitized logs or screenshots.

Do not include SSH passwords, private keys, access passwords, reconnect tokens, session IDs, or production connection details. Prefer the local fixture over a production host when sharing a reproduction.

The repository currently uses pull requests for proposed changes. Do not assume an issue tracker, discussion board, support SLA, or private disclosure channel has been configured.

## Before opening a pull request

- Run `npm run lint` and `npm run testonly` after building frontend resources.
- Run `npm run build` when code or dependencies change.
- Describe browser verification when UI behavior changes, including any untested paths.
- Update user-facing documentation if configuration, behavior, or build requirements change.
- Keep upstream attribution and existing license notices.

This project uses the license in [LICENSE.md](LICENSE.md). Contributions should be compatible with that license.
