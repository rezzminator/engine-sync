# engine-sync — the caller interface

What a program outside this repository (pfm, a hook script, CI, the fence) may rely on. The binary is the single writer of every Codex and OpenCode artifact derived from Claude sources; a caller execs it and never imports its packages. P1 output is byte-identical to `pfm codex` / `pfm opencode` at professor@881ab88f.

## Finding the binary

A caller outside the plugin resolves, in order:

1. `$ENGINE_SYNC_BIN`, when it names an executable regular file (a dev or fence build; never linked anywhere);
2. `{engine-sync home}/bin/engine-sync`, the stable path the plugin wrapper links forward-only to a cached version. `{engine-sync home}` is `$ENGINE_SYNC_HOME` when absolute, else `$XDG_DATA_HOME/engine-sync` when absolute, else `$HOME/.local/share/engine-sync`.

Neither present: the caller fails visibly with `engine-sync binary unavailable: {stable path}: {cause}` (exit 1, a failing doctor row, a failed install step); it never skips silently.

`engine-sync version` prints `engine-sync {version}` and exits 0.

## Exit codes

Every subcommand: `0` pass, `1` a finding or a failure, `2` a usage error (usage on stderr). With `--json`, stdout carries exactly one JSON object for exit 0 and 1; a usage error prints no JSON.

## Per-engine commands (the forwarders' targets)

`pfm codex {build|check|agents} ARGS…` forwards to `engine-sync codex {build|check|agents} ARGS…`; `pfm opencode {build|check|doctor} ARGS…` to `engine-sync opencode {build|check|doctor} ARGS…`. Same positionals, same flags, same meanings, same exit codes, same stdout:

| Command | Flags | stdout on pass |
| --- | --- | --- |
| `engine-sync codex build\|check [repo]` | `--home`, `--model alias=value` (repeatable), `--root-adapter`, `--agent-preamble`, `--exclude-dir`, `--exclude-project`, `--never-register` (repeatable each), `--suffix-mode`, `--suffix-prefix`, `--json` | `CODEX BUILD PASS` / `CODEX CHECK PASS` |
| `engine-sync codex agents` | `--home` | one `{path}: {n} B, parses clean` per compiled role, one `{STATE} {description}` per link and role, `CODEX AGENTS PASS` |
| `engine-sync opencode build\|check\|doctor [repo]` | `--home`, `--json` | `OPENCODE BUILD PASS wrote=N unchanged=N deleted=N` / `OPENCODE CHECK PASS` / `OPENCODE DOCTOR PASS` |

stderr lines keep today's text with the program prefix renamed: `engine-sync codex: {problem}`, `engine-sync codex: warning: {warning}`, `engine-sync opencode: {problem}`, `engine-sync codex agents: {problem}`. A repository root defaults to the nearest ancestor of the working directory holding `.professor/workbench.json`, else `.git`, else the first holding a regular `CLAUDE.md`.

`--json` replaces the human stdout and stderr with one object:

```json
{"ok": false, "problems": ["CONFLICT …"], "rebuildable": ["STALE …"], "warnings": [], "wrote": 0, "unchanged": 0, "deleted": 0, "error": ""}
```

`problems` holds every problem line; `rebuildable` the subset a `build` would repair (a multiset: a line listed twice is rebuildable twice); `error` is a run failure (the repository could not be read), with `ok` false.

## Both engines at once

`engine-sync build|check|doctor [repo] [--home H] [codex flags…]` runs the Codex then the OpenCode command of the same name (`doctor` runs `codex check` then `opencode doctor`), each printing as above; both run whatever the first returned; exit 1 when either did.

## `engine-sync global` — the installer's Codex steps

```text
engine-sync global [--home H] [--source-repo R] [--claude-config-dir C]… [--codex-home X]… [--no-codex-home]
                   [--source-home S] [--plan] [--uninstall] [--json]
```

- `--home` defaults to `$HOME`; `--source-repo` to `{home}/.professor`; `--claude-config-dir` (repeatable) to `{home}/.claude`; `--codex-home` (repeatable) to `{engine-sync config}/targets.json`'s `codexHomes`, else `$CODEX_HOME`, else `{home}/.codex`. `--no-codex-home` is the explicit empty roster (no Codex account configured). `--source-home` is the home whose `.claude/commands` the global commands compile from (default `--home`): the installer's preview stages its future commands there. `{engine-sync config}` is `$XDG_CONFIG_HOME/engine-sync` when absolute, else `{home}/.config/engine-sync`.
- `--plan` writes nothing and lists what an apply would do; an apply lists what it did. The two lists are the same list: the plan is the apply's preview.
- Without `--uninstall` it runs, in order: the global agents (every `{source-repo}/templates/global/agents/*.md` and declared variant compiled to `{codex home}/agents/{name}.toml` as a marker-owned regular file, the rendered variant sources, one link per agent in every `{claude config dir}/agents/`), the retirement of orphaned generated roles, of undeclared variants and their links, and of the renamed roles (`frr`, `rr-super`); then, with a Codex home, the global commands (`{home}/.codex/prompts`, `{home}/.codex/skills`) and the retired `explorer` role's strays. Without one it reports the two skips the installer printed.
- `--uninstall` retires every generated role in every Codex home, the generated agent stores, and every managed global command, creating nothing.

The human output is the installer's transcript lines (`  change  {message}`, `  ok      {message}`, `  skip    {message}`, `  conflict {message}`). `--json` prints:

```json
{"ok": true, "events": [{"verb": "change", "message": "link /h/.claude/agents/a.md -> /r/templates/global/agents/a.md"},
                        {"verb": "retire", "message": "retire /h/.codex/agents/x.toml (retired generated Codex agent)", "path": "/h/.codex/agents/x.toml"},
                        {"verb": "skip", "message": "…"}, {"verb": "ok", "message": "Codex global agents compiled=12 linked=12"},
                        {"verb": "conflict", "message": "…"}], "error": ""}
```

`verb` is one of `change`, `retire`, `ok`, `skip`, `conflict`; `message` is the installer's own wording, unchanged; `path` is set on `retire` only (the path removed, or to be removed). A `conflict` blocks: `ok` is false and the exit is 1; under `--plan` the rest of the plan is still listed. `error` names a failure that stopped the run.

## `engine-sync doctor --machine` — the machine-level rows

```text
engine-sync doctor --machine [--home H] [--source-repo R] [--claude-config-dir C] [--codex-home X]… [--claude-absent] [--json]
```

Classifies the Claude agents registry and every Codex agents registry against the source clone, the rows pfm doctor printed as `doctor: global-agents …`. No `--source-repo`: the no-clone state. Human output: one `{level} {message}` line per row. `--json`:

```json
{"ok": false, "rows": [{"level": "fail", "message": "global-agents source=/h/.codex state=MISSING names=a hint=\"run pfm install\""}]}
```

`level` is `ok`, `warn` or `fail`; exit 1 when any row fails. `engine-sync doctor` without `--machine` is the repository doctor above.

## `{engine-sync config}/targets.json`

```json
{"codexHomes": ["/h/.codex", "/h/.codex-2"], "claudeConfigDirs": ["/h/.claude"], "sourceRepo": "/h/.professor"}
```

`codexHomes` is required (an empty list is no Codex home); the other two are optional and default as for `global`. `pfm install` writes it; `hook claude start` refreshes the machine targets from it, and does nothing machine-level without it.

## `engine-sync hook claude {mark|stop|start}`

The Claude plugin's hooks, reading the hook payload on stdin. A project is in scope when it has `.claude/codex-build.json`, or its `AGENTS.md` or `.opencode/opencode.jsonc` carries a generated marker; out of scope, every hook exits 0 silently.

- `mark` (`PostToolUse`, `Write|Edit`): when `tool_input.file_path` is a Claude source (under `$HOME/.claude/commands/`, any `.claude/` directory, a `CLAUDE.md`, a `.mcp.json`), sets the project's dirty flag `{engine-sync state}/dirty/{project}-{key}`. The project root is `$CLAUDE_PROJECT_DIR`, else the file's git superproject, else its git toplevel. `{engine-sync state}` is `$ENGINE_SYNC_STATE` when absolute, else `$XDG_STATE_HOME/engine-sync` when absolute, else `$HOME/.local/state/engine-sync`.
- `stop` (`Stop`): with the flag set, runs `codex build`, `codex check`, `opencode build`, `opencode check`; all pass: clears the flag, prints nothing. Any fails: keeps the flag and prints `{"decision": "block", "reason": "engine-sync: {stages} failed …"}` naming each failed stage and carrying only its output, or under `stop_hook_active` `{"systemMessage": "engine-sync WARNING: {stages} still failing …"}`. Always exit 0.
- `start` (`SessionStart`, async): runs `check` and rebuilds a stale mirror; with `targets.json`, runs `global` over its targets. Exit 0.

The wrapper `plugins/engine-sync/libexec/engine-sync` under `hook` never fails a call: an unavailable binary exits 0, and under `hook claude stop` prints `{"systemMessage": "engine-sync WARNING: binary unavailable — mirrors were not checked; dirty flag retained ({cause})"}`.
