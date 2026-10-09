# Changelog

Every release of engine-sync. Versions follow [semantic versioning](https://semver.org); each release is the `main` commit tagged `v<version>`, with a GitHub release carrying the section below and the four platform binaries.

## [Unreleased]

### Added
- The `engine-sync` binary, extracted from pfm's `codex` and `opencode` compilers with their history: `codex build|check|agents`, `opencode build|check|doctor`, `build`, `check` and `doctor` over both engines, `doctor --machine`, `global` for the machine-level Codex roles, commands and Claude agent links, and `hook claude mark|stop|start`. Output is byte-identical to pfm's.
- A version guard: a binary refuses a project or Codex home a newer engine-sync last wrote (`NEWER-WRITER`), and writes to one root are serialized by a lock.
- The `engine-sync` Claude Code plugin: a `PostToolUse` hook marks a turn that wrote a Claude source, `Stop` rebuilds and checks both mirrors and blocks on a failing stage, `SessionStart` rebuilds a stale mirror and refreshes the machine targets `targets.json` names. Its wrapper resolves `$ENGINE_SYNC_BIN`, then the cached binary, then a checksum-verified release download, and keeps a stable `bin/engine-sync` link that only moves forward.
