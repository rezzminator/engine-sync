# engine-sync caller interface

A caller execs the binary; it never imports implementation packages or writes compiler-owned mirrors.

## Contents

- [Binary and exits](#binary-and-exits)
- [Project commands](#project-commands)
- [Policy resolution and schemas](#policy-resolution-and-schemas)
- [Agent frontmatter](#agent-frontmatter)
- [Variants](#variants)
- [Global commands](#global-commands)
- [Machine targets](#machine-targets)
- [Generated ownership](#generated-ownership)
- [Claude hooks](#claude-hooks)
- [Environment](#environment)

## Binary and exits

Resolve an executable regular `$ENGINE_SYNC_BIN` first, then `{engine-sync home}/bin/engine-sync`. An unavailable binary is a visible failure; no silent skip. `version` prints `engine-sync {version}`: `0.4.0-alpha` for development, the plugin manifest version for release assets.

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
| `global` | Machine roles, OpenCode agents and commands; `--plan`, `--uninstall`, `--adopt`, `--capability NAME`, account flags. |
| `variants` | `pin [name]…`; rewrite only the `pins:` block of source declarations or shared files (a shared file named by its path relative to the agents directory). |
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

Without an explicit root, resolve the nearest ancestor holding `.claude/engine-sync.json`, the configured workbench marker, `.git`, or a regular `CLAUDE.md`. A workbench suppresses global command writes and is excluded from its parent's project discovery. `ENGINE_SYNC_WORKBENCH_MARKER` overrides the resolved marker.

Project JSON result:

```json
{"ok":false,"problems":["CONFLICT …"],"rebuildable":["STALE …"],"warnings":[],"wrote":0,"unchanged":0,"deleted":0,"error":""}
```

Project commands also accept `--engine codex|opencode`; the neutral selection runs every target in the registry. `build --dry-run` runs the same read-only plan as `plan`. Plan exits 0 for repairable findings, 1 for conflicts or invalid artifacts, and 2 for usage errors. Plan JSON adds `actions`, an array of `{ "kind": "write|link|delete", "path": "…", "target": "…" }`; target is omitted when empty. Doctor JSON adds `configSources`, an array of `{ "key": "…", "value": "…", "source": "…" }`, and human doctor prints `CONFIG` rows. The existing result fields stay present.

`rebuildable` is the multiset subset of `problems` that build repairs. `error` is a run failure, with `ok` false.

## Policy resolution and schemas

One typed resolver validates configuration before any stage runs. Layers are built-in neutral defaults < `{engine-sync config}/config.json` < `{root}/.claude/engine-sync.json` < environment < flags. Maps merge by key, lists replace, and scalars replace. In `opencode.permission`, a lower bare action becomes the leading `*` fallback when a later layer adds tools. Tools and patterns merge across layers. A deny wins when both layers name the same pattern; more specific patterns follow broader ones. Absent policies and empty rule sets deny access. An absent file uses defaults; unknown keys, wrong types and unreadable files fail with the path and key. Project files cannot contain `machine`.

The legacy `.claude/codex-build.json` maps only onto `codex`, and the legacy `.claude/opencode-build.json` only onto `opencode.modelMap`; each is read only while `.claude/engine-sync.json` is absent, with one stderr deprecation notice. Configuration schema version is `1`. Development reports `0.4.0-alpha`; released assets report the plugin manifest version.

```sh
engine-sync config show --json .
engine-sync config validate .
engine-sync config schema
```

`config show` prints every effective leaf value and its source; JSON has `ok`, `config`, `sources`, and `error`. Each source row has `key`, `value`, and `source`. `doctor` also includes provenance. The generated [JSON Schema](engine-sync.schema.json) contains the shared machine shape and a project-only `$defs.project` shape. The loader additionally enforces the file's scope.

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
| `machine.sourceGeneratedAgentsDir` | `""` | Source-repository-relative directory declarations render into instead of `machine.generatedAgentsDir`, so the rendered agents live beside their sources; empty keeps the home store. `--uninstall` never removes it. |

Removed keys: `machine.modelMap` (use `codex.modelMap`) and `opencode.agent` (use each agent's `engines.opencode.permission`). A configuration carrying either fails to load, naming its replacement.

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

Global roles use the Claude configuration directory by default; `--source-repo R` selects `R/{machine.sourceAgentsDir}/`. An agent's `engines.codex` block overrides its role settings (see [Agent frontmatter](#agent-frontmatter)). `engines.codex.sandbox` accepts `inherit`, `read-only`, or `workspace-write`; otherwise a tools list without Write, Edit, MultiEdit or NotebookEdit selects read-only; Bash alone does not declare write intent. Use `sandbox: inherit` for a role that writes through Bash. No role name has special treatment. `codex.modelMap` maps model aliases for project and machine roles alike.

Generated file markers and rebuild hints name `engine-sync build`. MCP regions retain the unbranded legacy script fence understood by 0.1.0, including on a machine without writer records. Complete historical ownership headers remain evidence; partial marker phrases do not claim a file; rebuilding migrates owned mirrors in place and preserves unmarked foreign files.

## Agent frontmatter

Agent frontmatter is YAML, parsed by one parser for agents, commands, skills and rules; re-rendering keeps comments and key order. A header strict YAML refuses is read once more with each top-level single-line `key: value` whose line does not parse on its own taken as a double-quoted string, as Claude Code reads it (`description: Review: run the checks`); a header that still fails is the problem `FRONTMATTER-INVALID {file}:{line} — {error}` and the artifact is skipped, never dropped silently. The machine-readable shape is [agent.schema.json](agent.schema.json).

engine-sync's own keys are `engines`, `requires` and `role`. Claude Code's native sub-agent keys pass through; `agents.passthroughKeys` names extra keys a host accepts; any other key warns `UNKNOWN-KEY {file}: {key}`. `role` is stamped by the variant renderer only, so a source carrying it is a problem.

```yaml
engines:
  codex:                     # a map, or false: not emitted for Codex
    model: opus              # a key of codex.modelMap is mapped; any other value is emitted verbatim
    effort: high             # low|medium|high|xhigh|max (Codex receives max as xhigh)
    sandbox: workspace-write # inherit|read-only|workspace-write
  opencode:                  # a map, or false: not emitted for OpenCode
    model: opus              # a key of opencode.modelMap is mapped; any other value verbatim
    mode: subagent           # all|primary|subagent
    permission: {}           # rendered as the agent's OpenCode permission
requires: [example-capability]
```

Model precedence per engine is `engines.{engine}.model`, then the engine's model map applied to `model`, else the model is omitted with a warning. Codex effort is `engines.codex.effort`, then `effort`. An unknown setting under an engine, and a `claude` key under `engines`, are errors. The flat keys `codex-model`, `codex-effort`, `codex-sandbox`, `opencode-model` and `opencode-mode` are errors naming their `engines.{engine}.{setting}` home. An agent whose `requires` names a capability outside the host's set (see `--capability`) is neither linked nor emitted. The Codex role `name` is the frontmatter `name`, verbatim, in projects and on the machine. `rootAdapter` and `agentPreamble` templates receive `.Models`, the engine's model map, as in `{{index .Models "opus"}}`; an alias the map lacks fails the build, naming it.

## Variants

A machine source `{machine.sourceAgentsDir}/{name}.md` carrying `from:` declares a rendered agent. Its `name` must equal the kebab-case file base. `from` names an original, another declaration, or `general-purpose` (empty frontmatter and body). A cycle is an error naming the chain. `machine.variants` is refused with `{machine.sourceAgentsDir}/{name}.md declarations` as its replacement.

```yaml
---
name: reader-pro
from: reader
extends: [shared]
model: sonnet
add:
  tools: Write, Grep
unset: [skills]
retitle:
  Budget: Budget (pro)
pins:
  Budget: sha256:{hex}
prepend: |-
  Follow the return law.
---

## Budget (pro)

Spend more time reviewing.
```

A shared file is any `.md` below a subdirectory of the source agents directory, such as `shared/base.md`: it carries no `name` and no `from`, takes every other layer key, and is never an installed agent, a link or a doctor agent row. `extends: [shared/a.md, shared/b.md]` applies shared files in order, each named by its path relative to the declaring file and each shared file's own `extends` first; a bare name, a path outside a subdirectory, a missing file and a cycle are named errors. A shared file no declaration extends is `SHARED-UNUSED`, and the retired `abstract:` key is `DECLARATION-INVALID`. Layer order is the rendered `from` base, then `extends`, then the declaration itself.

Each layer applies `unset` (a missing dotted path is `UNSET-MISSING`), native frontmatter keys (maps deep-merge, scalars and lists replace), then `add` (lists append, maps deep-merge; a scalar target is an error). `name` and `role` cannot be set, added or unset: the compiler stamps `name` and `role: {root original}` after it, omitting role for `general-purpose`. `tools` accepts a list or comma string and renders `tools: A, B, C`. Controls `from`, `extends`, `add`, `unset`, `rename`, `prepend`, `append`, `pins` and `retitle` never reach rendered frontmatter.

Each real `## Heading` in the source body overrides the base's same-named section; fenced headings do not count. Nonempty text before the first heading overrides `(intro)`, preserving the base's leading blank line; empty intro inherits. `retitle: {old heading: new heading}` names the base section while the body writes the new heading. A section's extent runs to the next heading of the same or higher level. Its pin is `sha256:` followed by SHA-256 of its extent's lines joined with `\n`, leading and trailing blank lines removed. Missing, repeated, unpinned and changed base sections are `SECTION-MISSING`, `SECTION-AMBIGUOUS`, `SECTION-UNPINNED` and `SECTION-DRIFT` respectively. Review an override before running `engine-sync variants pin {name}`.

Later layers win section and rename keys. `rename` replaces whole-name occurrences in the body, simultaneously over the original body (`{a: b, b: c}` turns `a` into `b` and `b` into `c`); an unused key is `RENAME-UNUSED {name}: {key} matched 0 times`. `prepend` and `append` concatenate in layer order and join only nonempty parts with two newlines.

The rendered Claude file is written into `machine.sourceGeneratedAgentsDir` of the source repository when set, else into `machine.generatedAgentsDir`, and linked. When the source-repository directory is set, `global` retires every owned file left in the home store. `doctor --machine` names each rendered file whose declaration is gone in a failing `ORPHAN` row. Its marker credits the declaration file. Codex and OpenCode follow its rendered `engines`, and `requires` decides installation. `check` validates sources and rendered declarations in the configured source directory. Project `.claude/agents/*.md` files with `from:` or another declaration control are named problems: Claude Code would load them raw, so these controls are machine scope only.

`global` skips each broken source and installs healthy agents, exiting 1. A malformed header is `FRONTMATTER-INVALID`; invalid declarations and rendering failures are named by their own errors. An agent skipped for an error keeps its installed copies; only unmet `requires` retires one.

## Global commands

`--capability NAME` (repeatable) names a capability the host has; it is accepted by `global`, `codex agents`, `doctor --machine` and every project command (`build`, `check`, `doctor`, `plan`, `codex build|check`, `opencode build|check|doctor`). The capability set is the union of every `--capability` and the `capabilities` list of [targets.json](#machine-targets). Names carry no meaning to engine-sync: an agent whose frontmatter `requires` lists a name outside the set is neither linked nor emitted, owned artifacts left from an earlier run are retired (never an artifact engine-sync does not own), check prints `SKIPPED {agent} — requires {name}`, and machine doctor prints the agent's `agent SKIPPED all {agent} {path} (requires {name} unmet)` row.

```text
engine-sync global [--home H] [--source-repo R] [--claude-config-dir C]… [--codex-home X]…
                   [--no-codex-home] [--source-home S] [--plan] [--uninstall] [--adopt]
                   [--capability NAME]… [--json]
engine-sync codex agents [--home H] [--source-repo R] [--capability NAME]…
engine-sync doctor --machine [--home H] [--source-repo R] [--claude-config-dir C]
                            [--codex-home X]… [--claude-absent] [--capability NAME]… [--json]
engine-sync variants pin [name …] [--home H] [--source-repo R]
```

`--adopt` moves an unowned regular file at a declared agent's destination to `{engine-sync state}/adopted/{UTC timestamp}/{path relative to home}`, then writes the managed artifact in its place; without it the file stays a conflict. A file reached through two spellings moves once; a destination already taken gets the next free `{stem}.{n}{ext}`, so no earlier adopted file is replaced; ownership is checked again right before each move (a file gone, no longer regular, or now carrying the marker stays and is named); across filesystems the move is a copy, synced, then the original removed. `variants pin` recomputes section pins for named declarations or shared files (all when none is named), rewrites only their `pins:` block, and keeps every other byte unchanged. It prints `pinned {name} § {heading} {pin}` for each change. A shared file's rewrite is refused, naming the consumer, if it would make an unasked current declaration drift. A symlinked source is written through to its target.

Global role sources default to `machine.claudeAgentsDir`, whose neutral value is `${CLAUDE_CONFIG_DIR:-$HOME/.claude}/agents/` for all three commands. Explicit `--source-repo R` selects `R/{machine.sourceAgentsDir}/`; targets may select the repository for `global` and machine doctor. Global commands resolve only the machine policy file; a source repository supplies templates, never project policy. `--home` defaults to `$HOME`.

If the machine source agents directory physically resolves to any Claude agents directory being installed into, every source carrying `from:` is a named validation problem: Claude Code loads it raw there. engine-sync never renders, links over, replaces, moves or adopts it, including with `--adopt`. Set `sourceAgentsDir` (or `sourceRepo`) to a directory outside every Claude agents dir. Plain agents in the registry keep their existing behavior. Doctor reports the same validation problem, with no additional state.

`global` plans or applies role TOMLs, rendered variants, Claude agent links, retirements and the global command mirror. Codex role TOMLs are marker-owned regular files: Codex refuses symlinked roles. A plain source already in its destination Claude registry stays a regular file. With no source directory, global reports a skip; doctor names `NO-SOURCES` without clone-specific wording. A source agent global skips as broken is one doctor `BROKEN` row naming it, and every healthy agent's registries are still inspected.

`engines.codex.sandbox` accepts `inherit` (no sandbox line), `read-only`, or `workspace-write`. Otherwise a tools allow-list lacking write tools compiles read-only; omitted tools inherit. There is no agent-name exception.

OpenCode receives every machine-global agent, original and variant, as a marker-owned `{home}/.config/opencode/agents/{name}.md` unless its `engines.opencode` is `false`; a file whose source goes is retired.

`--plan` writes nothing. `--uninstall` retires managed roles, variants and commands, creating nothing. `--source-home` selects the home whose `.claude/commands` supplies global commands; default is `--home`. `--no-codex-home` supplies an explicit empty Codex roster.

Global human output is `change|ok|skip|conflict {message}`. JSON:

```json
{"ok":true,"events":[{"verb":"change","message":"write …"},{"verb":"retire","message":"retire …","path":"…"}],"error":""}
```

Machine doctor reports each declared agent once per engine registry as `agent {STATE} {engine} {name} {path}`: `INSTALLED` (present, owned, current), `MISSING`, `STALE` (owned but not current, or owned at an undeclared name), `SHADOWED` (an unowned file at the declared name), `SKIPPED` (engine `all`; a `requires` name is unmet), and `STRAY` (a warning: an unowned file at an undeclared name in the Codex or OpenCode registry; the shared Claude registry reports none). `INSTALLED` and `SKIPPED` rows are `ok`; a failing doctor still prints its `SKIPPED` rows. A `STALE` agent is one row: its planned retirement is not repeated. A machine config that fails to load is a `fail` row, and the agent rows give way to `global-agents states NOT SCANNED`: no directory is scanned from a config that never loaded.

`verb` is `change`, `retire`, `ok`, `skip`, or `conflict`; `path` appears only on retire. Conflicts clear `ok`; a plan continues listing them. Machine doctor JSON retains `ok` and `rows` and adds `configSources` with the same key/value/source rows as project doctor. Configuration failures print one object with `ok:false`, empty `rows` and `configSources`, and `error` naming the file and key. Row levels are `ok`, `warn`, `fail`, and a fail exits 1.

## Machine targets

Contract path: `{engine-sync config}/targets.json`.

```json
{"codexHomes":["/example/codex"],"claudeConfigDirs":["/example/claude"],"sourceRepo":"/example/repo","capabilities":["example-capability"]}
```

Every key is optional. `codexHomes` may be empty (no Codex home); a file without it means the same Codex homes as no targets file, so a capabilities-only file is valid. Explicit CLI values win over targets, which win over environment/defaults. Without targets, Codex homes default to `$CODEX_HOME` when absolute, else `{home}/.codex`. Claude config dirs default to `$CLAUDE_CONFIG_DIR`, else `{home}/.claude`. Target home and config-directory entries must be absolute. Hook start refreshes machine targets only when this file exists.

OpenCode agents take `engines.opencode.model` as an explicit model and `engines.opencode.mode` as `all`, `primary`, or `subagent`, and `engines.opencode.permission` as their permission; Codex settings affect only Codex. Native target validation checks the requested mode. Template `Source` is the project-relative source path.

## Generated ownership

Current markers are rendered by `internal/artifact/marker.go`: `Generated by engine-sync from {source} — edit the source, then run: engine-sync build`, and `… then run: engine-sync global` on a machine render — a rendered declaration, a Codex global role and an OpenCode machine agent. Markdown uses an HTML comment, TOML and YAML frontmatter use `#`, and JSONC uses `//`. Codex MCP fences begin `# BEGIN mcp_servers — generated by .claude/scripts/build-codex.mjs from .mcp.json; do not edit inside this fence` and end `# END mcp_servers — generated`.

Complete historical compiler headers remain ownership evidence; a quoted or partial marker does not. Links require a receipt proving engine-sync created that exact link; location or byte equality alone does not establish ownership. Old Codex MCP begin fences are recognized and replaced too. Global role ownership is anchored to its first line. Rebuild migrates old owned files in place; unmarked foreign files remain conflicts.

Writer records and locks live under `{engine-sync state}/writers/`. A root written by a newer binary refuses an older writer with `NEWER-WRITER`. Global writes use the `global:{home}` root.

## Claude hooks

`hook claude mark|stop|start` reads a hook JSON payload on stdin. Scope is a project having a new or legacy configuration, or an output carrying a recognized generated marker. Registered hooks and the wrapper exit 0; direct CLI usage errors exit 2; runtime failures appear in diagnostics or the hook response.

`mark` handles `tool_input.file_path` under a `.claude` directory, a `CLAUDE.md`, a `.mcp.json`, or the home's global commands. Root is the physical session root for sources inside it, otherwise the edited file's nearest configured marker, `.git`, or regular `CLAUDE.md`. A second project is marked independently; HOME and its ancestors are refused. It sets `{engine-sync state}/dirty/{project}-{key}`.

`stop` keeps its dirty flag durable while running build/check for each registered target. Success clears only the unchanged generation, under the dirty-store lock, preserving concurrent marks and process termination. Failure retains dirtiness and prints `{"decision":"block","reason":"…"}`; with `stop_hook_active`, prints `{"systemMessage":"…"}`. Reasons name the failed stages, binary and neutral repair commands.

`start` checks and repairs stale project mirrors and refreshes the common machine roster, including defaults. The wrapper runs hook binaries as children, preserves stdin and always exits zero. Child failure or invalid Stop JSON yields a named diagnostic and a Stop `systemMessage`. An unavailable binary reports that mirrors were not checked; it makes no claim that a dirty flag exists. Unavailable hooks save a deferred refresh for the physical session root; the next equipped hook in that project runs SessionStart recovery before handling its payload. Hook children terminate within 16 seconds. Failed downloads back off for 60 seconds; dead download owners and locks older than 120 seconds are reclaimed without waiting. Hook writer-lock waits are bounded to 250 ms and report BUSY without blocking the turn.

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

## Recovery

A NEWER-WRITER refusal names the binary and version last admitted to the canonical root. Use that binary or a newer compatible build. Development is newer than the published plugin; a released plugin cannot safely take over its roots. Writer records are proof, so removing them is an operator decision after confirming that a downgrade can consume the current format.

A conflict preserves the named file or link. Move a foreign output aside only after inspecting it. Keeper symlinks are retained; a fenced region is edited through a link only when its target stays inside the managed root. Garbage collection removes marker-owned files or recorded links, then empty directories; remaining foreign children are a conflict in plan as well as build. Source reads require regular files and are bounded to 8 MiB; ownership probes read at most 4 KiB.

Machine destination precedence is explicit roster flags (including an empty roster), absolute CODEX_HOME, targets.json, explicitly configured machine.codexHomes (including []), then the default Codex home. An explicit roster permits recovery past a broken targets file. Unknown or duplicate keys and relative registry paths are errors. Doctor, global, agents and SessionStart share this resolver and the writer guard. Home OpenCode commands are machine outputs under their own shared destination guard. Project policy never renders or collects those outputs.
