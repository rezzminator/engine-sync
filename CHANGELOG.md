# Changelog

Every release of engine-sync. Versions follow [semantic versioning](https://semver.org); each release is the `main` commit tagged `v<version>`, with a GitHub release carrying the section below and the four platform binaries.

## [Unreleased]

### Changed

- Development builds report `0.2.0-alpha` so a released binary refuses their roots as `NEWER-WRITER`.
- One source model, rewrite chain, target registry and artifact reconciler serve project and machine scopes. Added `plan`, `--engine`, `--dry-run`, `config show|validate|schema`, tracked JSON Schema, permanent goldens and mechanical tree gates with pinned staticcheck.

- Standalone defaults for both engines and global roles; project policy lives in validated `.claude/engine-sync.json`, in symmetric `codex` and `opencode` sections, with one-release compatibility for the previous Codex configuration file.
- Native ordered OpenCode permissions and per-agent overrides, configurable preambles, root-file mirrors, MCP server knowledge and workbench markers. Machine policy lives beside targets in `config.json`; doctor reports the source of each resolved value. Global agent stores and retirements are machine-only settings; literal `rewrites` support engine and artifact scopes.
- Global roles default to the Claude configuration directory. `codex agents` accepts `--source-repo`; `codex-sandbox` frontmatter controls sandbox exceptions for any role name.
- Generated markers and rebuild hints name engine-sync; rebuilding recognizes and migrates existing owned mirrors.
- Checks inspect only generated surfaces and native configuration files, leaving unrelated engine-home files alone. Inspection skips sockets, FIFOs and devices while reporting regular-file read and directory-walk errors.

## [0.1.0] - 2026-10-09

### Added

- The `engine-sync` binary, extracted from pfm's `codex` and `opencode` compilers with their history: `codex build|check|agents`, `opencode build|check|doctor`, `build`, `check` and `doctor` over both engines, `doctor --machine`, `global` for the machine-level Codex roles, commands and Claude agent links, and `hook claude mark|stop|start`. Output is byte-identical to pfm's.
- A version guard: a binary refuses a project or Codex home a newer engine-sync last wrote (`NEWER-WRITER`), and writes to one root are serialized by a lock.
- The `engine-sync` Claude Code plugin: a `PostToolUse` hook marks a turn that wrote a Claude source, `Stop` rebuilds and checks both mirrors and blocks on a failing stage, `SessionStart` rebuilds a stale mirror and refreshes the machine targets `targets.json` names. Its wrapper resolves `$ENGINE_SYNC_BIN`, then the cached binary, then a checksum-verified release download, and keeps a stable `bin/engine-sync` link that only moves forward.
