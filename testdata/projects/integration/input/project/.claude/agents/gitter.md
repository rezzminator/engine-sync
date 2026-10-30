---
name: gitter
description: The only fixture agent that writes git.
tools: Read, Bash, Glob, Grep
model: haiku
engines:
  codex:
    sandbox: inherit
  opencode:
    permission:
      bash:
        "git *": allow
---

Run the git write the caller orders, then verify the refs.
