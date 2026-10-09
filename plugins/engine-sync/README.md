# engine-sync

**Carries a project's Claude Code agents, commands, skills, rules and MCP servers to Codex and OpenCode, and keeps the mirrors in step after every turn**

## Install

```text
/plugin install engine-sync --marketplace rezzminator/engine-sync
```

## What it does

- After a turn that wrote a Claude source (anything under `.claude/`, a `CLAUDE.md`, a `.mcp.json`, a global command), the `Stop` hook rebuilds and checks the Codex mirror (`AGENTS.md`, `.codex/`) and the OpenCode mirror (`.opencode/`). A mirror that fails its check blocks the turn's end, naming the failing stage, so the model repairs it.
- At session start it rebuilds a mirror a `git pull` left stale, and refreshes the machine-level Codex roles when `~/.config/engine-sync/targets.json` names them.
- A project is in scope when it carries `.claude/codex-build.json` or a mirror engine-sync generated; every other project is left alone.

## The binary

Every hook runs `libexec/engine-sync`, a POSIX sh wrapper. It runs `$ENGINE_SYNC_BIN` when that names an executable, else the cached binary for this plugin version under `${ENGINE_SYNC_HOME:-${XDG_DATA_HOME:-$HOME/.local/share}/engine-sync}/versions/`, else downloads the release binary and checks it against `libexec/SHA256SUMS`. `bin/engine-sync` beside the cache is a stable path to the newest version the wrapper resolved, for callers outside Claude Code.

Source, design and issues: [github.com/rezzminator/engine-sync](https://github.com/rezzminator/engine-sync).

Built and maintained with [Professor](https://github.com/rezzminator/professor).
