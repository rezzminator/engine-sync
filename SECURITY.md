# Security Policy

## Supported versions

Only the latest release receives security fixes. `develop` carries the next pre-release; a fix lands there and ships in the next release.

## Reporting a vulnerability

Report privately through GitHub's
[private vulnerability reporting](https://github.com/rezzminator/engine-sync/security/advisories/new).
Do not open a public issue for a vulnerability.

Include the engine-sync version (`engine-sync version`), reproduction steps, and the impact you observed. You get an acknowledgement within 7 days and a fix-or-decline decision within 30.

## Scope

In scope: the `engine-sync` binary, the plugin under `plugins/engine-sync/` (hooks, wrapper and checksums), and the repository's GitHub Actions workflows.

Out of scope: vulnerabilities in Claude Code, Codex, OpenCode, or other third-party tools engine-sync writes for — report those to their maintainers.
