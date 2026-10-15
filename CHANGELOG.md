# Changelog

Every release of engine-sync. Versions follow [semantic versioning](https://semver.org); each release is the `main` commit tagged `v<version>`, with a GitHub release carrying the section below and the four platform binaries.

## [Unreleased]

### Added

- Plugin errors, warnings and meaningful info are bounded JSON rows in `errors.jsonl`, `warnings.jsonl` and `info.jsonl` under `$PLUGIN_LOG_DIR/engine-sync` (default `~/.local/state/engine-sync/logs`). Capped files move to `.1`, and a failed logging streak reports once on stderr without changing command results.

### Fixed

- Failed project `check` and `doctor` commands write error rows with their diagnostic cause and session.

- A machine-global OpenCode agent keeps the `mcp__{server}__{tool}` grants of the servers the machine config names in `mcp.globalServers`: the machine path passed no known servers, so every such grant was dropped as an unknown server. A configured server name renders in OpenCode's tool-part charset (letters, digits, `_`, `-`; any other character becomes `_`), as a project server's does, so a dotted name grants and no configured name reaches an agent file raw. A tool of a server named nowhere, or of a retired one, is named in a warning and its server is denied outright (`{server}_*: false`): OpenCode enables a tool an agent's map leaves out. This changes the bytes of a machine-global OpenCode agent: each one now denies every configured global server it does not grant (`{server}_*: false`).

## [0.3.0] - 2026-10-12

### Added

- Agent frontmatter carries per-engine settings in an `engines:` block (Codex model, effort and sandbox; OpenCode model, mode and permission; `false` skips an engine). One YAML node parser serves every source kind; a header failing strict YAML is retried once with single-line values read as literal strings, and one that still fails is `FRONTMATTER-INVALID {file}:{line}`, skipped by name.
- Derived machine agents are declaration files: a machine source carrying `from:` declares one rendered agent whose Claude-native keys and `## Heading` body sections replace the base's; `retitle`, `pins`, `add`, `unset`, `rename`, `prepend` and `append` shape it, and `variants pin` rewrites a file's `pins:` block in place. A declaration extends a shared `.md` below a subdirectory by relative path; a shared file nothing extends is reported `SHARED-UNUSED`.
- Machine agents are capability-gated: `requires` is checked against `--capability` and the targets.json `capabilities` list, and machine GPT agents install only when asked for.
- `machine.sourceGeneratedAgentsDir` (source-repository-relative, empty by default) renders declarations beside their sources instead of the home store; links point there, `global` retires what it owned in the home store, and `--uninstall` never removes the repository directory.
- Machine doctor is closed-world: `INSTALLED`, `MISSING`, `STALE`, `SHADOWED`, `SKIPPED`, `BROKEN`, `STRAY`, and a failing `ORPHAN` row for each rendered file whose declaration is gone. `global --adopt` moves shadowing hand files to the adopted store; one broken agent is skipped by name and keeps its installed copy.

### Changed

- Rendered machine agents carry a `role` stamp and a mark naming `engine-sync global`; OpenCode global agents are written under `agents/`. Project Codex role names are the frontmatter name verbatim; targets.json `codexHomes` is optional.
- `check` and `doctor` see the whole Claude roster for the source collision; `check` skips a directory holding no declaration. A declaration inside a Claude agents directory engine-sync installs into is a named problem and is never replaced.

### Removed

- `--gpt-agents`: use `--capability`. Flat `codex-*` and `opencode-*` agent keys, the `machine.variants` carrier (variants.json), `machine.modelMap`, `opencode.agent` and `abstract:` are refused, each naming its replacement: port each variants.json entry to its own `{machine.sourceAgentsDir}/{name}.md` declaration and each abstract to a shared file it extends.

### Fixed

- `global --adopt` change events carry no `path`, as docs/interface.md's JSON contract says.
- Reconciliation retires only assets engine-sync owns; a registry link with no receipt that points at an engine-sync-generated file is recognized as ours, so a moved render repoints it, and a moved home store is retired only after the registry links repoint, so no link dangles into it.

## [0.2.0] - 2026-10-12

### Added

- One cached source model, rewrite chain, target registry and artifact reconciler serve project and machine scopes. Added `plan`, `--engine`, build-only `--dry-run`, `config show|validate|schema`, JSON Schema, permanent goldens and mechanical tree gates with pinned staticcheck and Claude Code validation.
- Configurable machine source/marker directories, command rosters and Codex review model, effort and scope prompts; the standalone defaults contain no integrator protocol or layout. Project and machine policy report their provenance.

### Changed

- Unmapped Codex models warn and omit the model pin; configure a model map to retain a pin. The historical agent-name sandbox exemption is removed: use explicit `codex-sandbox: inherit` when write access is intended. Removing `.mcp.json` removes its managed MCP fence while preserving hand-written keeper content.
- Layered OpenCode permission patterns retain denies from either layer; a deny wins equal-pattern conflicts and more specific patterns follow broad rules. Missing policies and empty rule sets deny access. Project `bash` entries no longer erase machine git-write denies.
- Codex keeper MCP regions retain the unbranded legacy script fence syntax readable by 0.1.0 on a fresh machine; the immediately preceding development fence remains accepted for migration.
- Download locks recover dead owners immediately and expire after 120 seconds. Unavailable hooks save a project-scoped deferred refresh; the next equipped hook in that project runs SessionStart recovery. Hook children are bounded and Stop notices include busy-lock details.
- Development builds report a prerelease of the next version (`0.3.0-alpha` once 0.2.0 is released); the version gate requires a prerelease newer than the plugin manifest. Release builds and four native CI jobs execute assets to verify the reported version matches their names.
- Neutral model maps, adapters, agent preambles, MCP knowledge, retirement lists and generated-store defaults replace integrator-specific defaults. Configure previous values to retain their bytes; changed defaults intentionally alter unconfigured mirrors.
- Global commands render only from machine sources and policy, under machine guards, including during project builds. One resolver selects explicit homes, CODEX_HOME, targets, configured homes and defaults; an explicit empty roster disables Codex homes.
- Configured Go agent-preamble templates now interpolate fields; legacy root adapters retain literal shell-style placeholders. Project command references also use the configured machine roster, so known global slash commands translate in project bodies. Dotted filenames are preserved instead of being mistaken for commands.
- Native OpenCode model pins and reserved permission keys are quoted; absent policies and empty permission rule sets deny access. Agent names, suffixes and exclusions are shared across engines, with full Unicode case/NFC collision checks and path validation. Codex effort aliases map max/ultra to xhigh; TOML control characters are escaped.
- Codex model comments say `model` instead of `tier`. Unresolvable-source notices say `output kept`. Generated markers and rebuild hints name engine-sync; JSONC markers identify configuration and MCP sources. Complete historical headers remain recognized.
- Native ordered OpenCode permissions and agent overrides come from configuration. Lower bare actions survive as leading wildcard rules, JSONC preserves user keys, and a missing command description warns per file. BOM, CRLF and spaced frontmatter fences are parsed; malformed or non-UTF-8 sources fail visibly.
- Generated ownership requires a complete header or a recorded link receipt. Unproven copies and hand-made aliases are preserved, including historical links whose location alone does not prove ownership. Garbage collection preserves foreign children, scratch files and unresolvable outputs. New parents use 0755; keeper modes and contained keeper links survive updates. Replacing changed generated content emits a notice.
- Source reads are limited to 8 MiB and ownership reads to a header; special files fail before open. Canonical bounded roots, ancestor checks, bounded hook locks and partial-write records prevent escapes and repeated obsolete writes.
- The wrapper rechecks cached checksums, backs off downloads, serializes stable-link changes, preserves stdin, rejects invalid Stop JSON and always exits zero for hooks. Dirty flags remain durable throughout Stop stages and concurrent edits survive flag cleanup.

### Fixed

- Multi-home retargeting, machine doctor/installer disagreement, silent write/config/discovery errors, invalid dry-run success, contradictory usage and ownership diagnostics, output collisions, template failures, and release/gate producer errors. Baselines may only shrink, including ordinary CI checks; empty enumerations and missing publication inputs report ERROR.
- Dead duplicate CLI/config/reconcile adapters and their tests were removed; surviving OpenCode CLI tests use the production route. Documentation now states current scope, recovery, byte changes and the checks actually enforced.

## [0.1.0] - 2026-10-09

### Added

- The `engine-sync` binary, extracted from pfm's `codex` and `opencode` compilers with their history: `codex build|check|agents`, `opencode build|check|doctor`, `build`, `check` and `doctor` over both engines, `doctor --machine`, `global` for the machine-level Codex roles, commands and Claude agent links, and `hook claude mark|stop|start`. Output is byte-identical to pfm's.
- A version guard: a binary refuses a project or Codex home a newer engine-sync last wrote (`NEWER-WRITER`), and writes to one root are serialized by a lock.
- The `engine-sync` Claude Code plugin: a `PostToolUse` hook marks a turn that wrote a Claude source, `Stop` rebuilds and checks both mirrors and blocks on a failing stage, `SessionStart` rebuilds a stale mirror and refreshes the machine targets `targets.json` names. Its wrapper resolves `$ENGINE_SYNC_BIN`, then the cached binary, then a checksum-verified release download, and keeps a stable `bin/engine-sync` link that only moves forward.
