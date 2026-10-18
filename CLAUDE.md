# engine-sync — carries a project's Claude Code configuration to Codex and OpenCode

engine-sync is the single writer of every Codex and OpenCode artifact derived from Claude sources, in every project and Codex home on the host: a wrong byte it writes lands in every mirror at the next turn's `Stop`, and a newer copy's records refuse an older one.

# Vocabulary

- binary: the Go program behind every command in `docs/interface.md` § Commands · `cmd/engine-sync/` · contract `docs/interface.md`
- configuration: validated project `.claude/engine-sync.json` and global `~/.claude/engine-sync.json`, with provenance and schema · `internal/config/`
- source model: one Claude parser and discovery, shared across targets · `internal/source/`
- declaration: a machine agent source carrying `from:` · `internal/source/declarations.go` · contract `docs/interface.md` § Declarations
- shared file: an `.md` below a subdirectory of the source agents directory, layered into a declaration through `extends` · `internal/source/declarations.go` · contract `docs/interface.md` § Declarations
- generated agent: the Claude agent file a declaration renders to; render is the verb · `internal/source/declarations.go` · contract `docs/interface.md` § Declarations
- variant: spelled only in the published `variants pin` verb and the printed `VARIANT-INVALID`; everywhere else, declaration
- Codex compiler: `AGENTS.md`, `.codex/**`, the role TOMLs and the global commands · `internal/target/codex/`
- OpenCode compiler: `.opencode/**` · `internal/target/opencode/`
- machine scope: the machine-level Codex roles, Claude agent links and both targets' commands, with their retirements and `doctor --machine` rows, where an owned undeclared file marked by `machine.managedBy` fails `ORPHAN` · `internal/machine/`
- version guard: the per-root writer record and lock; an older binary refuses a root a newer one wrote (`NEWER-WRITER`) · `internal/guard/`, records under `{engine-sync state}/writers/`
- `{engine-sync home}`: `$ENGINE_SYNC_HOME`, else `$XDG_DATA_HOME/engine-sync`, else `~/.local/share/engine-sync`; holds `versions/{version}/engine-sync` and the stable link `bin/engine-sync` · `internal/paths/paths.go`
- `{engine-sync state}`: `$ENGINE_SYNC_STATE`, else `$XDG_STATE_HOME/engine-sync`, else `~/.local/state/engine-sync`; writer records, locks, dirty flags, link and write receipts
- `targets`: the Codex homes and Claude config dirs `global` writes, plus the source clone and capabilities · the `targets` object of `~/.claude/engine-sync.json`
- plugin: what installs into Claude Code · `plugins/engine-sync/`
- marketplace: the plugin listing · `.claude-plugin/marketplace.json`
- manifest: the plugin's version · `plugins/engine-sync/.claude-plugin/plugin.json`
- hooks: `mark` after a Write or Edit, `stop`, and `start` (async) · `plugins/engine-sync/hooks/hooks.json`
- wrapper: the POSIX sh entry every hook runs; resolves `$ENGINE_SYNC_BIN` (never linked), then the cache, then a checksum-verified download pinned in `plugins/engine-sync/libexec/SHA256SUMS` · `plugins/engine-sync/libexec/engine-sync`
- `testjail`: every test package's `TestMain` points `HOME`, `TMPDIR` and the XDG roots into temp dirs and clears every engine-sync and Claude env var; `RanUnprivileged` re-runs, under root, a test whose fixture is a permission bit as uid 65534 · `internal/testjail/testjail.go`
- wrapper tests: the wrapper against a fake binary and a local release server · `internal/wrappertest/`
- leak gate: the identifying-content scan over a private, untracked terms file · `scripts/leak-check.sh`

# Runtime

## Local

- Affected: `go test ./internal/{pkg}/ -run {Test} -count=1`, or `./cmd/engine-sync/`
- Full: `go test ./...`; permanent project and machine fixtures live in `testdata/projects/`.
- Golden: `scripts/golden-projects.py --binary PATH --fixture {plain|professor}` rewrites `testdata/projects/{fixture}/golden/artifacts.json`.
- Tree: `scripts/check-tree_test.sh`, then `LEAK_TERMS={checkout}/scripts/leak-terms.txt scripts/check-tree.sh` (`LEAK_TERMS` defaults to `{checkout}/scripts/leak-terms.txt`). With no terms supplied, `publication` prints GAP and the run exits 3 when nothing else failed: no pass, no red; a named unusable file is ERROR.
- Static: install the staticcheck version in `scripts/tool-versions.txt`, then `staticcheck ./...`.
- Lint: `go vet ./...`, and `"$(go env GOROOT)/bin/gofmt" -l .` printing nothing.
- Leak gate: `scripts/leak-check.sh`; in a worktree, `LEAK_TERMS={checkout}/scripts/leak-terms.txt scripts/leak-check.sh`.
- Fence: a parent project's dev container that mounts this checkout may export that project's own terms as `LEAK_TERMS`; there, set it inline to `{checkout}/scripts/leak-terms.txt` for the tree and leak gates, else they scan against the wrong denylist.
- Validate: `claude plugin validate --strict .` (the marketplace) and `claude plugin validate --strict plugins/engine-sync` (the plugin)
- Scratch: `/tmp/engine-sync/{purpose}/`

## CI

- `.github/workflows/ci.yml` on every push to `{develop|main}` and every pull request: gofmt, vet, pinned staticcheck, tree gates, tests, both validates, the leak gate. Fork pull requests run the public gates; private leak terms are available only to trusted runs.

# Rules

## Sacred ground

- **Byte identity:** a compiler change that alters a mirror's bytes is a release note, never a refactor; markers are rendered only in `internal/artifact/marker.go`; legacy headers are read-only ownership claims, never emitted.
- **Publication:** NEVER push, tag or release without the user's explicit ask in the current turn.
- **Nothing identifying** ships in a tracked file: no machine-absolute path, no private term; the leak gate is the backstop.
- **The real home:** a test NEVER reads or writes the real `$HOME`, Codex home or Claude config dir; `testjail` refuses to run on it.

## Git

- Git writes go through the `gitter` agent.
- Work lands on `develop`; `main` moves only by release.

## Plugin

- `plugins/engine-sync/` holds only what installs: manifest, hooks, wrapper, checksums, README and LICENSE; implementation source stays outside it.
- A hook never fails a turn by its exit code: every hook exits 0; a failing stage answers a `Stop` block or a `systemMessage`.
- Text the model reads (hook output, block reasons) stays unbranded; Professor's credit lives only in the READMEs and the `author` and `keywords` of the manifest and the marketplace.
