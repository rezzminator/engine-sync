# engine-sync

**Carries a project's Claude Code agents, commands, skills, rules and MCP servers to Codex and OpenCode, and keeps the mirrors in step after every turn**

[![CI](https://github.com/rezzminator/engine-sync/actions/workflows/ci.yml/badge.svg?branch=develop)](https://github.com/rezzminator/engine-sync/actions/workflows/ci.yml)
[![Version](https://img.shields.io/badge/version-0.1.0-blue)](./CHANGELOG.md)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](./LICENSE)
[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://docs.claude.com/en/docs/claude-code/plugins)
[![Built with Professor](https://img.shields.io/badge/built%20with-Professor-8A2BE2)](https://github.com/rezzminator/professor)

Claude Code is the source: `CLAUDE.md`, `.claude/agents`, `.claude/commands`, `.claude/skills`, `.mcp.json`. engine-sync compiles them into what Codex reads (`AGENTS.md`, `.codex/`, role TOMLs in each Codex home) and what OpenCode reads (`.opencode/`), and is the single writer of every one of those files. Codex and OpenCode install nothing.

## Commands

```text
engine-sync codex build|check [root]     the Codex mirror
engine-sync codex agents                 the machine-level Codex roles
engine-sync opencode build|check|doctor [root]
engine-sync build|check|doctor [root]    both engines
engine-sync doctor --machine             the machine-level rows
engine-sync global [--plan] [--uninstall] [--codex-home H]…
engine-sync hook claude mark|stop|start  the plugin's hooks
engine-sync version
```

Exit codes: 0 pass, 1 a failed check or build, 2 a usage error. `--json` prints one result object. The full contract is `docs/interface.md`.

## The plugin

`plugins/engine-sync/` is the Claude Code plugin: after a turn that wrote a Claude source, `Stop` rebuilds and checks both mirrors and blocks the turn's end on a failing stage; `SessionStart` rebuilds a mirror a `git pull` left stale. Its wrapper, `libexec/engine-sync`, finds the binary for the plugin's version (`$ENGINE_SYNC_BIN`, the cache, then a release download checked against `libexec/SHA256SUMS`).

## Where things live

- `${ENGINE_SYNC_HOME:-${XDG_DATA_HOME:-$HOME/.local/share}/engine-sync}`: `versions/{version}/engine-sync`, and the stable link `bin/engine-sync`.
- `${ENGINE_SYNC_STATE:-${XDG_STATE_HOME:-$HOME/.local/state}/engine-sync}`: writer records, locks and dirty flags.
- `${XDG_CONFIG_HOME:-$HOME/.config}/engine-sync/targets.json`: the Codex homes and Claude config dirs `global` writes.

## Develop

```sh
go vet ./... && go test ./...
claude plugin validate --strict . && claude plugin validate --strict plugins/engine-sync
```

Built and maintained with [Professor](https://github.com/rezzminator/professor).
