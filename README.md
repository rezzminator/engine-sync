# engine-sync

**Carries a project's Claude Code agents, commands, skills and MCP servers to Codex and OpenCode, and keeps the mirrors in step after every turn**

[![CI](https://github.com/rezzminator/engine-sync/actions/workflows/ci.yml/badge.svg?branch=develop)](https://github.com/rezzminator/engine-sync/actions/workflows/ci.yml)
[![Version](https://img.shields.io/badge/version-0.2.0-blue)](./CHANGELOG.md)
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
| `variants` | `pin [name]…`; rewrite only the `pins:` block of source declarations or abstracts. |
| `hook` | `claude mark`, `stop`, `start`; hook protocol on stdin/stdout. |
| `version` | Print the binary version; `--version` is an alias. |
| `help` | Print usage and registered targets; `-h` and `--help` are aliases. |

Exit codes: 0 pass, 1 a finding or failure, 2 a usage error. The complete flag and JSON contract is [interface.md](docs/interface.md).

## The plugin

`plugins/engine-sync/` is the Claude Code plugin: after Write or Edit marked an in-scope Claude source, `Stop` rebuilds and checks both mirrors and blocks the turn's end on a failing stage; `SessionStart` rebuilds a mirror a `git pull` left stale. Its wrapper, `libexec/engine-sync`, finds the binary for the plugin's version (`$ENGINE_SYNC_BIN`, a cache reverified on every adoption, then a bounded release download checked against `libexec/SHA256SUMS`).

## Where things live

- `${ENGINE_SYNC_HOME:-${XDG_DATA_HOME:-$HOME/.local/share}/engine-sync}`: `versions/{version}/engine-sync`, and the stable link `bin/engine-sync`.
- `${ENGINE_SYNC_STATE:-${XDG_STATE_HOME:-$HOME/.local/state}/engine-sync}`: writer records, locks and dirty flags.
- `${XDG_CONFIG_HOME:-$HOME/.config}/engine-sync/targets.json`: the Codex homes and Claude config dirs `global` writes; `config.json` beside it holds machine policy.

## Standalone use

A project needs only its Claude sources. Build the mirrors once, then use the plugin hooks to keep them current:

```sh
go build -o engine-sync ./cmd/engine-sync
./engine-sync build .
./engine-sync check .
./engine-sync doctor .
./engine-sync global --plan
```

No project configuration is required. Global roles come from `${CLAUDE_CONFIG_DIR:-$HOME/.claude}/agents/`; `global` and `doctor --machine` use the same source. `--source-repo R` selects `R/{machine.sourceAgentsDir}/` instead, including for `codex agents`. Plain sources already in their destination registry stay regular files; they are never replaced by links to themselves. Declarations (`from:`) and shared abstracts (`abstract: true`) must live outside every Claude agents registry; collocated controls are refused without changing their bytes. See [Variants](docs/interface.md#variants).

## Configuration

One typed resolver validates configuration before any stage runs. Layers are built-in neutral defaults < `{engine-sync config}/config.json` < `{root}/.claude/engine-sync.json` < environment < flags. Maps merge by key, lists replace, and scalars replace. In `opencode.permission`, a lower bare action becomes the leading `*` fallback when a later layer adds tools. Tools and patterns merge across layers. A deny wins when both layers name the same pattern; more specific patterns follow broader ones. Absent policies and empty rule sets deny access. An absent file uses defaults; unknown keys, wrong types and unreadable files fail with the path and key. Project files cannot contain `machine`.

The legacy `.claude/codex-build.json` maps only onto `codex`, and the legacy `.claude/opencode-build.json` only onto `opencode.modelMap`; each is read only while `.claude/engine-sync.json` is absent, with one stderr deprecation notice. Configuration schema version is `1`. Development reports `0.3.0-alpha.2`; released assets report the plugin manifest version.

```sh
engine-sync config show --json .
engine-sync config validate .
engine-sync config schema
```

`config show` prints every effective leaf value and its source; JSON has `ok`, `config`, `sources`, and `error`. Each source row has `key`, `value`, and `source`. `doctor` also includes provenance. The generated [JSON Schema](docs/engine-sync.schema.json) contains the shared machine shape and a project-only `$defs.project` shape. The loader additionally enforces the file's scope.

| Key | Neutral default | Meaning |
| --- | --- | --- |
| `version` | `1` | Required in each file. |
| `commandDirs` | `[]` | Additional project-relative command source directories. |
| `codex.review.modelAlias` | `""` | Review model alias; empty inherits the engine model. |
| `codex.review.defaultEffort` | `""` | Effort for a bare review invocation; empty inherits. |
| `codex.review.prompt` | `""` | Optional review scope prompt. |
| `codex.review.slotPrompt` | `""` | Optional scope prompt for the effort slot. |
| `machine.sourceAgentsDir` | `"agents"` | Agents directory relative to an explicit source repository. |
| `machine.agentMarkerDir` | `""` | Marker source directory; empty derives from the source. |
| `codex.globalCommands` | `true` | Include machine Claude commands in the project mirror. |
| `agents.passthroughKeys` | `[]` | Extra agent frontmatter keys this host accepts without an `UNKNOWN-KEY` warning. |
| `codex.modelMap` | `{}` | Alias to native Codex model for project and machine roles; unmapped aliases warn and roles omit model, inheriting the engine default. |
| `codex.rootAdapter` | `Go template (text below)` | Go template appended to the generated root orientation. |
| `codex.agentPreamble` | `Go template (text below)` | Go template prepended to project role instructions. |
| `codex.excludeDirs` | `[]` | Source command subtrees excluded from discovery. |
| `codex.projects` | `Automatic discovery` | Omitted: discover children; explicit empty list: root only. |
| `codex.excludeProjects` | `[]` | Child project names excluded from automatic discovery. |
| `codex.neverRegister` | `[]` | Role basenames omitted from native role registration. |
| `codex.suffixMode` | `"project"` | Role naming: an agent's frontmatter `name`, suffixed in a nested project by project, none, or strip-prefix. |
| `codex.suffixPrefix` | `""` | Prefix removed in strip-prefix mode. |
| `opencode.modelMap` | `{}` | Unmapped aliases warn; roles omit model. |
| `opencode.agentPreamble` | `Go template (text below)` | Go template prepended to OpenCode role instructions. |
| `opencode.permission` | `"deny"` | Native action or ordered tool rules; preserve pattern order (last match wins). |
| `opencode.mirrorFiles` | `[]` | Root-relative regular files mirrored into .opencode. |
| `workbenchMarker` | `""` | Optional root-relative workbench marker; empty disables it. |
| `mcp.globalServers` | `[]` | Additional machine-level MCP server names available to roles. |
| `mcp.renamed` | `{}` | Old server name to current server name; project-local old names remain valid. |
| `rewrites` | `[]` | Ordered literal rules; lists replace between layers. |
| `machine.claudeAgentsDir` | `{Claude config dir}/agents` | Defaults to the resolved Claude config directory plus `/agents`; home-relative or absolute. |
| `machine.legacyCodexAgentsDir` | `".local/state/engine-sync/generated/codex-agents"` | Home-relative owned store retired during migration. |
| `machine.retiredAgents` | `[]` | Obsolete machine role names retired from owned registries. |
| `machine.retiredFiles` | `[]` | Home-relative paths, including backups; configured retired skill paths suppress model-invocable command skills. |
| `machine.rewrites` | `[]` | Ordered rules applied at machine scope. |
| `machine.codexHomes` | `[]` | Unconfigured: environment/default Codex home; configured empty list: no homes. |
| `machine.generatedAgentsDir` | `".local/state/engine-sync/generated/claude-agents"` | Home-relative generated variant store; separate from source agents. |

Adapters and preambles use Go `text/template` fields `{{.Name}}`, `{{.Engine}}` (`Codex` or `OpenCode`), `{{.Source}}`, and `{{.Root}}` (`AGENTS.md`). Unknown fields and malformed templates fail at entry. Legacy `${name}` in a preamble is converted with one deprecation notice; in `codex.rootAdapter` it remains literal for compatibility. The neutral preamble is shared by both targets:

```text
You are the {{.Name}} role in this repository, running as a native {{.Engine}} subagent.
Read the repository root {{.Root}} before starting your task.
```

The neutral root adapter is:

```text
## {{.Engine}} adapter — reading this file in the {{.Engine}} harness

This file is compiled from {{.Source}} by engine-sync build. Translate harness mechanics to the matching {{.Engine}} command or role: edit the source and rebuild.
```

A project example:

```json
{"version":1,"codex":{"modelMap":{"smart":"example-model"},"agentPreamble":"Role {{.Name}} in {{.Engine}}. Read {{.Root}}.\n\n"},"opencode":{"permission":{"*":"allow","edit":{"private/**":"deny"}}},"mcp":{"globalServers":[],"renamed":{"old-tools":"tools"}},"rewrites":[{"engines":["codex"],"kinds":["agent"],"literal":"old protocol","replace":"current protocol"}],"workbenchMarker":""}
```

Each rewrite has `engines` and `kinds` lists (empty or omitted means all), a nonempty `literal`, and `replace`. Kinds are `agent`, `command`, `skill`, and `orientation`; links carry no emitted body to rewrite. Rewrites apply after built-in rules, in declaration order.

A machine example:

```json
{"version":1,"machine":{"claudeAgentsDir":".claude/agents","generatedAgentsDir":".local/state/engine-sync/generated/claude-agents","legacyCodexAgentsDir":".local/state/engine-sync/generated/codex-agents","codexHomes":["/example/codex"],"retiredAgents":["old-reader"],"retiredFiles":[".codex/skills/old-workflow"],"rewrites":[{"engines":["codex"],"kinds":["agent"],"literal":"old protocol","replace":"current protocol"}]}}
```

Global roles use the Claude configuration directory by default; `--source-repo R` selects `R/{machine.sourceAgentsDir}/`. An agent's `engines.codex` block overrides its role settings (see [Agent frontmatter](docs/interface.md#agent-frontmatter)). `engines.codex.sandbox` accepts `inherit`, `read-only`, or `workspace-write`; otherwise a tools list without Write, Edit, MultiEdit or NotebookEdit selects read-only; Bash alone does not declare write intent. Use `sandbox: inherit` for a role that writes through Bash. No role name has special treatment. `codex.modelMap` maps model aliases for project and machine roles alike.

Generated file markers and rebuild hints name `engine-sync build`. MCP regions retain the unbranded legacy script fence understood by 0.1.0, including on a machine without writer records. Complete historical ownership headers remain evidence; partial marker phrases do not claim a file; rebuilding migrates owned mirrors in place and preserves unmarked foreign files.

## Develop

```sh
go vet ./... && go test ./...
claude plugin validate --strict . && claude plugin validate --strict plugins/engine-sync
```

Built and maintained with [Professor](https://github.com/rezzminator/professor).

Machine command mirrors use machine policy and the common destination roster, independently of project policy. Native output directories cannot redirect writes through symlinks. Orphan cleanup removes owned files and empty directories; foreign children remain with a named conflict. See the [caller interface](docs/interface.md) for recovery and target precedence.
