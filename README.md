# engine-sync

**Carries a project's Claude Code agents, commands, skills and MCP servers to Codex and OpenCode, and keeps the mirrors in step after every turn**

[![CI](https://github.com/rezzminator/engine-sync/actions/workflows/release.yml/badge.svg?branch=develop)](https://github.com/rezzminator/engine-sync/actions/workflows/release.yml)
[![Version](https://img.shields.io/badge/version-0.4.1-blue)](./CHANGELOG.md)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](./LICENSE)
[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://docs.claude.com/en/docs/claude-code/plugins)
[![Built with Professor](https://img.shields.io/badge/built%20with-Professor-8A2BE2)](https://github.com/rezzminator/professor)

Claude Code is the source: `CLAUDE.md`, `.claude/agents`, `.claude/commands`, `.claude/skills`, `.mcp.json`. engine-sync compiles them into what Codex reads (`AGENTS.md`, `.codex/`, role TOMLs in each Codex home) and what OpenCode reads (`.opencode/`), and is the single writer of every one of those files. Codex and OpenCode install nothing.

## Commands

| Command | Purpose |
| --- | --- |
| `build` | Compile project artifacts; `--engine NAME`, `--dry-run` and project flags. |
| `check` | Read-only project findings; `--engine NAME` and project flags. |
| `doctor` | Project findings and configuration sources, or `--machine` rows. |
| `plan` | Read-only actions; `--engine NAME`, `--json` and project flags. |
| `codex` | `build`, `check`, `agents`; the existing Codex alias and role interface. |
| `opencode` | `build`, `check`, `doctor`; the existing OpenCode alias. |
| `config` | `show`, `validate`, `schema`; effective settings, validation and schema. |
| `global` | Machine roles, OpenCode agents and commands; `--plan`, `--uninstall`, `--adopt`, `--capability NAME`, account flags. |
| `variants` | `pin [name]…`; rewrite only the `pins:` block of source declarations or shared files (a shared file named by its path relative to the agents directory). |
| `hook` | `claude mark`, `stop`, `start`; hook protocol on stdin/stdout. |
| `version` | Print the binary version; `--version` is an alias. |
| `help` | Print usage and registered targets; `-h` and `--help` are aliases. |

Exit codes: 0 pass, 1 a finding or failure, 2 a usage error. The complete flag and JSON contract is [interface.md](docs/interface.md).

## The plugin

`plugins/engine-sync/` is the Claude Code plugin: after Write or Edit marked an in-scope Claude source, `Stop` rebuilds and checks both mirrors and blocks the turn's end on a failing stage; `SessionStart` rebuilds a mirror a `git pull` left stale. Its wrapper, `libexec/engine-sync`, finds the binary for the plugin's version (`$ENGINE_SYNC_BIN`, a cache reverified on every adoption, then a bounded release download checked against `libexec/SHA256SUMS`).

## Where things live

- `${ENGINE_SYNC_HOME:-${XDG_DATA_HOME:-$HOME/.local/share}/engine-sync}`: `versions/{version}/engine-sync`, and the stable link `bin/engine-sync`.
- `${ENGINE_SYNC_STATE:-${XDG_STATE_HOME:-$HOME/.local/state}/engine-sync}`: writer records, locks and dirty flags.
- `$HOME/.claude/engine-sync.json`: the global file. Its `machine` object holds machine policy and its `targets` object the Codex homes and Claude config dirs `global` writes.

## Logs

Errors, warnings and meaningful info are JSON rows in `errors.jsonl`, `warnings.jsonl` and `info.jsonl` under `$PLUGIN_LOG_DIR/engine-sync` when `PLUGIN_LOG_DIR` is set; otherwise they use `~/.local/state/engine-sync/logs`. Capped files move to `.1`; a failed logging streak reports once on stderr without changing command results.

## Standalone use

A project needs only its Claude sources. Build the mirrors once, then use the plugin hooks to keep them current:

```sh
go build -o engine-sync ./cmd/engine-sync
./engine-sync build .
./engine-sync check .
./engine-sync doctor .
./engine-sync global --plan
```

No project configuration is required. Global roles come from `${CLAUDE_CONFIG_DIR:-$HOME/.claude}/agents/`; `global` and `doctor --machine` use the same source. `--source-repo R` selects `R/{machine.sourceAgentsDir}/` instead, including for `codex agents`. Plain sources already in their destination registry stay regular files; they are never replaced by links to themselves. Declarations (`from:`) and their shared files must live outside every Claude agents registry; collocated controls are refused without changing their bytes. See [Declarations](docs/interface.md#declarations).

## Configuration

Configuration is one typed resolver over built-in defaults, machine policy, project policy, environment and flags, validated before any stage runs. The key table, layer rules, config commands, schemas, adapter templates and examples live in [Policy resolution and schemas](docs/interface.md#policy-resolution-and-schemas).

## Develop

```sh
go vet ./... && go test ./...
claude plugin validate --strict . && claude plugin validate --strict plugins/engine-sync
```

Built and maintained with [Professor](https://github.com/rezzminator/professor).

Machine command mirrors use machine policy and the common destination roster, independently of project policy. Native output directories cannot redirect writes through symlinks. Orphan cleanup removes owned files and empty directories; foreign children remain with a named conflict. See the [caller interface](docs/interface.md) for recovery and target precedence.
