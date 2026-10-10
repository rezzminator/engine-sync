# engine-sync caller interface

A caller execs the binary; it never imports implementation packages or writes compiler-owned mirrors.

## Contents

- [Binary and exits](#binary-and-exits)
- [Project commands](#project-commands)
- [Policy resolution and schemas](#policy-resolution-and-schemas)
- [Global commands](#global-commands)
- [Machine targets](#machine-targets)
- [Generated ownership](#generated-ownership)
- [Claude hooks](#claude-hooks)
- [Environment](#environment)

## Binary and exits

Resolve an executable regular `$ENGINE_SYNC_BIN` first, then `{engine-sync home}/bin/engine-sync`. An unavailable binary is a visible failure; no silent skip. `version` prints `engine-sync 0.1.0`.

Every command exits `0` on pass, `1` on a finding or failure, `2` on usage error. Hooks preserve the hook protocol described below. `--json` prints exactly one stdout object for exits 0 and 1; usage errors print no JSON. Legacy-config deprecation notices go to stderr even with `--json`.

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
| `global` | Machine roles and commands; `--plan`, `--uninstall`, account flags. |
| `hook` | `claude mark`, `stop`, `start`; hook protocol on stdin/stdout. |
| `version` | Print the binary version; `--version` is an alias. |
| `help` | Print usage and registered targets; `-h` and `--help` are aliases. |

## Project commands

| Command | Flags | Human stdout on pass |
| --- | --- | --- |
| `codex build\|check [repo]` | `--home`, `--model alias=value`, `--root-adapter`, `--agent-preamble`, `--exclude-dir`, `--exclude-project`, `--never-register`, `--suffix-mode`, `--suffix-prefix`, `--json` | `CODEX BUILD PASS` / `CODEX CHECK PASS` |
| `opencode build\|check\|doctor [repo]` | `--home`, `--json` | `OPENCODE BUILD PASS wrote=N unchanged=N deleted=N` / `OPENCODE CHECK PASS` / `OPENCODE DOCTOR PASS` |
| `build\|check\|doctor [repo]` | Both-engine options above | Codex then OpenCode reports |

Repeatable Codex flags are `--model`, `--exclude-dir`, `--exclude-project`, `--never-register`. Flags may precede or follow the positional root. Both engines run even if the first fails; `doctor` runs Codex check then OpenCode doctor.

Without an explicit root, resolve the nearest ancestor holding `.claude/engine-sync.json`, the environment workbench marker or `.git`, else the first regular `CLAUDE.md`. A workbench suppresses global command writes and is excluded from its parent's project discovery. `ENGINE_SYNC_WORKBENCH_MARKER` overrides the resolved marker.

Project JSON result:

```json
{"ok":false,"problems":["CONFLICT …"],"rebuildable":["STALE …"],"warnings":[],"wrote":0,"unchanged":0,"deleted":0,"error":""}
```

Project commands also accept `--engine codex|opencode`; the neutral selection runs every target in the registry. `build --dry-run` runs the same read-only plan as `plan`. Plan exits 0 for repairable findings, 1 for conflicts or invalid artifacts, and 2 for usage errors. Plan JSON adds `actions`, an array of `{ "kind": "write|link|delete", "path": "…", "target": "…" }`; target is omitted when empty. Doctor JSON adds `configSources`, an array of `{ "key": "…", "value": "…", "source": "…" }`, and human doctor prints `CONFIG` rows. The existing result fields stay present.

Project commands also accept `--engine codex|opencode`; the neutral selection runs every target in the registry. `build --dry-run` runs the same read-only plan as `plan`. Plan exits 0 for repairable findings, 1 for conflicts or invalid artifacts, and 2 for usage errors. Plan JSON adds `actions`, an array of `{ "kind": "write|link|delete", "path": "…", "target": "…" }`; target is omitted when empty. Doctor JSON adds `configSources`, an array of `{ "key": "…", "value": "…", "source": "…" }`, and human doctor prints `CONFIG` rows. The existing result fields stay present.

`rebuildable` is the multiset subset of `problems` that build repairs. `error` is a run failure, with `ok` false.

## Policy resolution and schemas

One typed resolver validates configuration before any stage runs. Layers are built-in neutral defaults < `{engine-sync config}/config.json` < `{root}/.claude/engine-sync.json` < environment < flags. Maps merge by key, lists replace, and scalars replace. In `opencode.permission` and each `opencode.agent.{name}.permission`, tools merge by name, and a layer that names a tool replaces that tool's whole ordered rule set in place, keeping tool order: a project adding one `bash` rule restates every `bash` rule it keeps. An absent file uses defaults; unknown keys, wrong types and unreadable files fail with the path and key. Project files cannot contain `machine`.

The legacy `.claude/codex-build.json` maps only onto `codex`, and the legacy `.claude/opencode-build.json` only onto `opencode.modelMap`; each is read only while `.claude/engine-sync.json` is absent, with one stderr deprecation notice. Configuration schema version is `1`; the release remains `0.1.0`.

```sh
engine-sync config show --json .
engine-sync config validate .
engine-sync config schema
```

`config show` prints every effective leaf value and its source; JSON has `ok`, `config`, `sources`, and `error`. Each source row has `key`, `value`, and `source`. `doctor` also includes provenance. The generated [JSON Schema](engine-sync.schema.json) contains the shared machine shape and a project-only `$defs.project` shape. The loader additionally enforces the file's scope.

| Key | Neutral default | Meaning |
| --- | --- | --- |
| `version` | `1` | Required in each file. |
| `codex.globalCommands` | `true` |  |
| `codex.modelMap` | `{}` | Unmapped aliases warn; roles omit model and inherit the engine default. |
| `codex.rootAdapter` | `Go template (text below)` |  |
| `codex.agentPreamble` | `Go template (text below)` |  |
| `codex.excludeDirs` | `[]` |  |
| `codex.projects` | `Automatic discovery` | Omitted: discover children; explicit empty list: root only. |
| `codex.excludeProjects` | `[]` |  |
| `codex.neverRegister` | `[]` |  |
| `codex.suffixMode` | `"project"` |  |
| `codex.suffixPrefix` | `""` |  |
| `opencode.modelMap` | `{}` | Unmapped aliases warn; roles omit model. |
| `opencode.agentPreamble` | `Go template (text below)` |  |
| `opencode.permission` | `{"*":"allow"}` | Native action or ordered tool rules; preserve pattern order (last match wins). |
| `opencode.agent` | `{}` | Overrides keyed by agent name, each with a native `permission` value. |
| `opencode.mirrorFiles` | `[]` |  |
| `workbenchMarker` | `""` |  |
| `mcp.globalServers` | `[]` |  |
| `mcp.renamed` | `{}` |  |
| `rewrites` | `[]` | Ordered literal rules; lists replace between layers. |
| `machine.modelMap` | `{}` |  |
| `machine.claudeAgentsDir` | `{Claude config dir}/agents` | Defaults to the resolved Claude config directory plus `/agents`; home-relative or absolute. |
| `machine.legacyCodexAgentsDir` | `".local/state/engine-sync/generated/codex-agents"` |  |
| `machine.retiredAgents` | `[]` |  |
| `machine.retiredFiles` | `[]` | Home-relative paths, including backups; configured retired skill paths suppress model-invocable command skills. |
| `machine.rewrites` | `[]` | Ordered rules applied at machine scope. |
| `machine.codexHomes` | `[]` | Unconfigured: environment/default Codex home; configured empty list: no homes. |
| `machine.variants` | `""` | Optional variants declaration, absolute or relative to the agents source directory; empty uses `variants.json`. |
| `machine.generatedAgentsDir` | `".local/state/engine-sync/generated/claude-agents"` | Home-relative generated variant store; separate from source agents. |

Adapters and preambles use Go `text/template` fields `{{.Name}}`, `{{.Engine}}` (`Codex` or `OpenCode`), `{{.Source}}`, and `{{.Root}}` (`AGENTS.md`). Unknown fields and malformed templates fail at entry. Legacy `${name}` is converted by the loader with one deprecation notice. The neutral preamble is shared by both targets:

```text
You are the {{.Name}} role in this repository, running as a native {{.Engine}} subagent.
First action: read the repository root {{.Root}} in full. Follow its laws and the protocol below exactly; your mode and task come from the dispatch prompt.
```

The neutral root adapter is:

```text
## {{.Engine}} adapter — reading this file in the {{.Engine}} harness

This file is compiled from {{.Source}} by engine-sync build. Translate harness mechanics to the matching {{.Engine}} command or role: edit the source and rebuild.
```

A project example:

```json
{"version":1,"codex":{"modelMap":{"smart":"example-model"},"agentPreamble":"Role {{.Name}} in {{.Engine}}. Read {{.Root}}.\n\n"},"opencode":{"permission":{"*":"allow","edit":{"private/**":"deny"}},"agent":{"writer":{"permission":{"edit":{"private/**":"ask"}}}}},"mcp":{"globalServers":[],"renamed":{"old-tools":"tools"}},"rewrites":[{"engines":["codex"],"kinds":["agent"],"literal":"old protocol","replace":"current protocol"}],"workbenchMarker":""}
```

Each rewrite has `engines` and `kinds` lists (empty or omitted means all), a nonempty `literal`, and `replace`. Kinds are `agent`, `command`, `skill`, and `orientation`; links carry no emitted body to rewrite. Rewrites apply after built-in rules, in declaration order.

A machine example:

```json
{"version":1,"machine":{"claudeAgentsDir":".claude/agents","generatedAgentsDir":".local/state/engine-sync/generated/claude-agents","legacyCodexAgentsDir":".local/state/engine-sync/generated/codex-agents","codexHomes":["/example/codex"],"modelMap":{"smart":"example-model"},"retiredAgents":["old-reader"],"retiredFiles":[".codex/skills/old-workflow"],"variants":"variants.json","rewrites":[{"engines":["codex"],"kinds":["agent"],"literal":"old protocol","replace":"current protocol"}]}}
```

Global roles use the Claude configuration directory by default; `--source-repo R` selects `R/templates/global/agents/`. Agent frontmatter `codex-model`, `codex-effort`, and `codex-sandbox` override role settings. Sandbox accepts `inherit`, `read-only`, or `workspace-write`; otherwise a tools list without write tools selects read-only. No role name has special treatment.

All generated markers and rebuild hints name `engine-sync build`. Previous markers remain ownership evidence; rebuilding migrates owned mirrors in place and preserves unmarked foreign files.

## Global commands

```text
engine-sync global [--home H] [--source-repo R] [--claude-config-dir C]… [--codex-home X]…
                   [--no-codex-home] [--source-home S] [--plan] [--uninstall] [--json]
engine-sync codex agents [--home H] [--source-repo R]
engine-sync doctor --machine [--home H] [--source-repo R] [--claude-config-dir C]
                            [--codex-home X]… [--claude-absent] [--json]
```

Global role sources default to `machine.claudeAgentsDir`, whose neutral value is `${CLAUDE_CONFIG_DIR:-$HOME/.claude}/agents/` for all three commands. Explicit `--source-repo R` selects `R/templates/global/agents/`; targets may select the repository for `global` and machine doctor. Global commands resolve only the machine policy file; a source repository supplies templates, never project policy. `--home` defaults to `$HOME`.

`global` plans or applies role TOMLs, rendered variants, Claude agent links, retirements and the global command mirror. Codex role TOMLs are marker-owned regular files: Codex refuses symlinked roles. A source already in its destination Claude registry stays a regular file. With no source directory, global reports a skip; doctor names `NO-SOURCES` without clone-specific wording.

Agent frontmatter `codex-sandbox` accepts `inherit` (no sandbox line), `read-only`, or `workspace-write`. Otherwise a tools allow-list lacking write tools compiles read-only; omitted tools inherit. There is no agent-name exception. The marker-only migration of a previously exempt role uses `codex-sandbox: inherit` to retain its bytes.

`--plan` writes nothing. `--uninstall` retires managed roles, variants and commands, creating nothing. `--source-home` selects the home whose `.claude/commands` supplies global commands; default is `--home`. `--no-codex-home` supplies an explicit empty Codex roster.

Global human output is `change|ok|skip|conflict {message}`. JSON:

```json
{"ok":true,"events":[{"verb":"change","message":"write …"},{"verb":"retire","message":"retire …","path":"…"}],"error":""}
```

`verb` is `change`, `retire`, `ok`, `skip`, or `conflict`; `path` appears only on retire. Conflicts clear `ok`; a plan continues listing them. Machine doctor JSON retains `ok` and `rows` and adds `configSources` with the same key/value/source rows as project doctor. Configuration failures print one object with `ok:false`, empty `rows` and `configSources`, and `error` naming the file and key. Row levels are `ok`, `warn`, `fail`, and a fail exits 1.

## Machine targets

Contract path: `{engine-sync config}/targets.json`.

```json
{"codexHomes":["/example/codex"],"claudeConfigDirs":["/example/claude"],"sourceRepo":"/example/repo"}
```

`codexHomes` is required and may be empty. The other keys are optional. Explicit CLI values win over targets, which win over environment/defaults. Without targets, Codex homes default to `$CODEX_HOME` when absolute, else `{home}/.codex`. Claude config dirs default to `$CLAUDE_CONFIG_DIR`, else `{home}/.claude`. Target home and config-directory entries must be absolute. Hook start refreshes machine targets only when this file exists.

OpenCode agent frontmatter accepts `opencode-model` as an explicit native model ID and `opencode-mode` as `all`, `primary`, or `subagent`; these override its generic model alias and mode. Codex keys affect only Codex. Native target validation checks the requested mode. Template `Source` is the project-relative source path.

## Generated ownership

Current markers are rendered by `internal/artifact/marker.go`: `Generated by engine-sync from {source} — edit the source, then run: engine-sync build`. Markdown uses an HTML comment, TOML and YAML frontmatter use `#`, and JSONC uses `//`. Codex MCP fences begin `# BEGIN mcp_servers — generated by engine-sync from .mcp.json — edit the source, then run: engine-sync build` and end `# END mcp_servers — generated`.

The old `Generated by pfm codex build`, `Generated by pfm opencode build`, and `.claude/scripts/build-{codex,opencode}.mjs` markers remain ownership evidence. Old Codex MCP begin fences are recognized and replaced too. Global role ownership is anchored to its first line. Rebuild migrates old owned files in place; unmarked foreign files remain conflicts.

Writer records and locks live under `{engine-sync state}/writers/`. A root written by a newer binary refuses an older writer with `NEWER-WRITER`. Global writes use the `global:{home}` root.

## Claude hooks

`hook claude mark|stop|start` reads a hook JSON payload on stdin. Scope is a project having a new or legacy configuration, or an output carrying a recognized generated marker. Every hook exits 0; runtime failures appear in diagnostics or the hook response.

`mark` handles `tool_input.file_path` under a `.claude` directory, a `CLAUDE.md`, a `.mcp.json`, or the home's global commands. Root is `$CLAUDE_PROJECT_DIR`, else the file's Git superproject or toplevel. It sets `{engine-sync state}/dirty/{project}-{key}`.

`stop` claims a dirty flag and runs Codex build, Codex check, OpenCode build, OpenCode check. Success clears only the claimed flag, preserving concurrent marks. Failure retains dirtiness and prints `{"decision":"block","reason":"…"}`; with `stop_hook_active`, prints `{"systemMessage":"…"}`. Reasons name the failed stages, binary and neutral repair commands.

`start` checks and repairs stale project mirrors and refreshes machine targets when present. The plugin wrapper's unavailable-binary Stop response is a `systemMessage`; dirty flags remain.

## Environment

| Variable | Meaning and fallback |
| --- | --- |
| `ENGINE_SYNC_BIN` | Caller/wrapper binary override; executable regular file |
| `ENGINE_SYNC_HOME` | Absolute cache home; else absolute `XDG_DATA_HOME/engine-sync`, else `$HOME/.local/share/engine-sync` |
| `ENGINE_SYNC_STATE` | Absolute state root; else absolute `XDG_STATE_HOME/engine-sync`, else `$HOME/.local/state/engine-sync` |
| `XDG_CONFIG_HOME` | Absolute config root; else `$HOME/.config`; append `engine-sync` |
| `ENGINE_SYNC_WORKBENCH_MARKER` | Relative workbench marker overriding either file; empty means no override |
| `CLAUDE_CONFIG_DIR` | Global Claude roles source and default destination config directory; else `$HOME/.claude` |
| `CODEX_HOME` | Default machine Codex target when absolute |
| `CLAUDE_PROJECT_DIR` | Hook project root |
| `HOME` | Default source/output home |
