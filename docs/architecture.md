# engine-sync architecture

## Contents

1. Shape — engine-sync is a compiler
2. The pipeline, stage by stage
3. Configuration — layers, provenance, schema
4. The integration surface — how Professor (or anyone) customises it
5. Ownership — markers, reconcile, the writer guard
6. Package tree
7. Glossary
8. Façades
9. Gates
10. Verification
11. Acceptance
12. Open rulings decided here

## 1. Shape — engine-sync is a compiler

One input language (a project's Claude Code sources) and N output targets (today Codex and OpenCode), joined by a typed intermediate model. Every run, at both scopes, is the same five stages:

```
settings ──► sources ──► render (per target) ──► plan ──► reconcile ──► report
 (config)    (source)    (rewrite + target/*)   (artifact)  (reconcile)   (diag)
```

- A stage reads only the stage before it. Only `source` reads Claude files; only `reconcile` touches output paths; `render` is pure (model + settings in, artifacts + diagnostics out, no filesystem).
- Two scopes run the identical pipeline: project (one repository root, its child projects) and machine (the Claude config dir's agents and commands → every Codex home in `targets.json`). They differ in source root, target roots and settings layers, never in code path.
- A new engine is one new `internal/target/{engine}/` package implementing the `Target` interface (§ 2.3) and one registry line. Nothing else changes.

## 2. The pipeline, stage by stage

### 2.1 settings — `internal/config`

Resolves one immutable `config.Config` per run (§ 3). The seat's layered resolver, schema validation and `Describe` source rows are the starting point and stay.

### 2.2 sources — `internal/source`

Reads the Claude project once into a typed model; the only package that parses a Claude file.

```go
type Project struct {
    Root        string
    Scope       Scope            // Project | Machine
    Orientation []Orientation    // every CLAUDE.md, root first, with its relative dir
    Agents      []Agent
    Commands    []Command        // name is the Claude spelling, colon-nested ("flights:spec")
    Skills      []Skill          // directory sources, linked, never copied
    MCP         []MCPServer      // .mcp.json, decoded and validated once
    Children    []Project        // child projects discovered once (excludes, workbench markers applied here)
    Unresolved  []Unresolved     // dangling symlink sources: the kept-twin rule lives here
}
type Agent struct {
    Name, Path  string
    Front       Frontmatter      // typed: Description, Model, Effort, Tools []string, Engine map[string]map[string]string
    Body        string
}
```

- One frontmatter parser (`source/frontmatter.go`) serves every target. Engine-prefixed keys (`codex-model`, `codex-effort`, `codex-sandbox`, `opencode-*`) land in `Front.Engine["codex"]["model"]` — the generic per-engine override channel; a target reads only its own map.
- One discovery (`source/discover.go`) finds projects, sources and the command roster for every target. The command roster is a field of the model, not recomputed per engine.
- A source that cannot be read is a diagnostic with its path, never an absent agent.

### 2.3 render — `internal/rewrite`, `internal/target`, `internal/target/{codex,opencode}`

```go
// internal/target/target.go
type Target interface {
    Name() string                                    // "codex", "opencode" — the engine package's spelling
    Roots(p source.Project, cfg config.Config) []artifact.Root   // the paths this target owns, for orphan detection
    Render(p source.Project, cfg config.Config) ([]artifact.Artifact, []diag.Diagnostic)
    Validate(a []artifact.Artifact) []diag.Diagnostic             // TOML parses, JSONC parses, doctor surfaces
}
var Registry = []Target{codex.Target{}, opencode.Target{}}      // the one list; CLI --engine and usage derive from it
```

- `rewrite` is the text middleware: an ordered chain of rules applied to every body and description a target emits.

  ```go
  type Rule interface { Apply(text string, ctx Context) string }   // ctx: engine, artifact kind, source path, roster, model map
  ```

  Built-in rules, in order: model-alias swap (`swapModels`), command-reference rewrite (the engine supplies its naming function: Codex `$flat-name` skill, OpenCode `/flat-name`), built-in command mapping (Codex's `/code-review` → `codex review …`, `target/codex/review.go`, owned by `target/codex` as a rule it registers), then the user's configured `rewrites` (§ 4). One command-swap implementation serves both targets.
- Templates: `rootAdapter` and `agentPreamble` are Go `text/template` strings over a documented field set — `{{.Name}}` (role name), `{{.Engine}}` (`Codex`/`OpenCode`), `{{.Source}}` (source path), `{{.Root}}` (`AGENTS.md`). The legacy `${name}` spelling is accepted and rewritten to `{{.Name}}` by the loader with one deprecation line. A template that fails to parse is a config error naming the key. The neutral default preamble is one engine-agnostic template (`… running as a native {{.Engine}} subagent …`); the `strings.ReplaceAll(DefaultAgentPreamble, "Codex", "OpenCode")` derivation in `config.Default` is deleted.
- Each target splits by artifact kind, one file each: `agents.go`, `commands.go`, `skills.go`, `mcp.go`, `orientation.go` (AGENTS.md + adapter), `config.go` (`.codex/config.toml` keeper region; `opencode.jsonc`), `permission.go` (OpenCode only), `validate.go`, `target.go`.

### 2.4 plan — `internal/artifact`

```go
type Artifact struct {
    Path    string
    Kind    Kind          // File | Link | Region (an owned fenced region inside a file engine-sync does not own)
    Content []byte        // File, Region
    Target  string        // Link
    Mode    fs.FileMode
    Source  string        // the Claude source it came from, for the marker and diagnostics
    Syntax  Syntax        // Markdown | TOML | JSONC | YAMLFront — picks the marker form
}
```

- The marker is rendered in exactly one place, `artifact/marker.go`, one form per `Syntax`: `Generated by engine-sync from {source} — edit the source, then run: engine-sync build`. `artifact.Claims(header)` recognises engine-sync's marker and every legacy marker (`Generated by pfm codex build`, `Generated by pfm opencode build`, `Generated by pfm from templates/global/agents/…`), so files written by older writers are rewritten in place, never `CONFLICT` or `ORPHAN`.

### 2.5 reconcile — `internal/reconcile`

One reconciler for both targets and both scopes: desired artifacts + target roots vs disk → actions and findings.

- Modes: `Build` (apply), `Check` (read-only, findings only), `Plan` (read-only, every action it would take — backs `global --plan` and a new `--dry-run`).
- States, one enum, one spelling everywhere (text, JSON, tests): `MISSING`, `STALE`, `ORPHAN`, `CONFLICT`, `UNPARSEABLE`, `INVALID`, `DANGLING`, `KEPT`. `rebuildable` = the states `Build` repairs.
- Writes go through `atomicfile`; concurrent writers through `guard` (per-root lock + writer-version record, unchanged).
- Machine-scope links and roles are classified here too (`reconcile/link.go`, `reconcile/role.go`), as artifact kinds at machine scope.

### 2.6 report — `internal/diag`

```go
type Diagnostic struct {
    Severity Severity   // Error | Finding | Warning
    State    reconcile.State  // when it is a reconcile finding
    Path, Source, Message, Remedy string
}
```

One renderer for human text and for `--json` (the result object in `docs/interface.md` stays byte-compatible: `ok`, `problems`, `rebuildable`, `warnings`, `wrote`, `unchanged`, `deleted`, `error`). "Nothing there" and "could not look" are different severities and print differently.

## 3. Configuration — layers, provenance, schema

- Layers, lowest first: built-in neutral defaults < machine file `{engine-sync config home}/config.json` < project file `{root}/.claude/engine-sync.json` (or legacy `.claude/codex-build.json`, mapped onto `codex` by the loader alone, one stderr deprecation line) < environment `ENGINE_SYNC_*` < flags. Maps merge by key, lists replace (a list never half-merges), scalars replace.
- Provenance: every leaf carries its layer and file. `engine-sync config show [--json] [root]` prints the effective value and source of every key; `doctor` includes the rows; `config validate [root]` validates every layer and exits 1 naming file and key.
- Scope: keys that describe one repository (adapter, preamble, excludes, permission, rewrites) are valid in both files; machine-only keys (`machine.*`: Claude agents dir, Codex homes default, retired roles and files, variants file) are valid only in the machine file — in a project file they are an error naming the key.
- Schema, one source: the Go structs with their tags. `engine-sync config schema` prints a JSON Schema (draft 2020-12) generated from them; the repository tracks it at `docs/engine-sync.schema.json` and a test regenerates-and-diffs it (registry parity). README's key table is generated from the same walk or checked against it.
- Neutral defaults: no vendor model id in code. With no model map, a compiled role omits `model` and the engine uses its own configured default; an alias with no mapping is a warning naming it. Professor's model map lives in Professor's files. The OpenCode default permission is `{"*": "allow"}` with no denies; no default mirror files; no known MCP servers beyond the project's `.mcp.json`.

Schema, project file (complete key set; `codex.*` are today's root-level Codex keys):

```json
{
  "version": 1,
  "codex": {
    "modelMap": {}, "rootAdapter": "…", "agentPreamble": "…",
    "excludeDirs": [], "projects": [], "excludeProjects": [], "neverRegister": [],
    "suffixMode": "project", "suffixPrefix": "", "globalCommands": true
  },
  "opencode": {
    "modelMap": {}, "agentPreamble": "…",
    "permission": { "edit": {}, "bash": {}, "*": "allow" },
    "agent": { "{name}": { "permission": {} } },
    "mirrorFiles": []
  },
  "mcp": { "globalServers": [], "renamed": {} },
  "rewrites": [ { "engines": ["codex","opencode"], "kinds": ["agent","command","skill","orientation"], "literal": "…", "replace": "…" } ],
  "workbenchMarker": ""
}
```

Machine file adds `"machine": { "generatedAgentsDir", "legacyCodexAgentsDir", "claudeAgentsDir", "codexHomes", "modelMap", "retiredAgents", "retiredFiles", "rewrites", "variants" }` (the seat's `global` block, renamed to its scope). `version: 1` is this unpublished schema; legacy `.claude/codex-build.json` loads with a deprecation line. The seat's draft keys survive unchanged except `global` → `machine` and `Global.BodyReplacements` → `rewrites`.

## 4. The integration surface — how Professor (or anyone) customises it

Everything Professor-specific is data in Professor's files, through five channels, each generic:

| Channel | What it carries | Professor's use |
| --- | --- | --- |
| Settings layers (§ 3) | adapters, preambles, model maps, excludes, MCP knowledge | Professor's adapter prose, gpt tier map, `professor` global MCP server and its renames |
| Native permission (`opencode.permission`, `opencode.agent.{name}.permission`) | OpenCode's own `permission` shape, passed through in order | guarded-file edit denies, the Git-verb bash denies, gitter's per-agent allow |
| Frontmatter engine keys (`codex-*`, `opencode-*`) | per-role engine settings | `codex-sandbox: inherit` on gitter; codex model and effort pins |
| `rewrites` | ordered literal replacements, scoped by engine and artifact kind | today's `Global.BodyReplacements` (spawn-agent substitution in global roles) |
| Machine file `machine.*` | where global roles come from, retirements, variants | `retiredAgents`, `retiredFiles`, Professor's `variants.json` |

- No code path names a Professor agent, file, server or phrase; the gate `spelling-professor` (§ 9) holds that at zero in non-test code and in every fixture output except the `professor` golden fixture.
- No exec'd extension hook: every Professor customisation measured today is expressible in the five channels. Rejected: a `postRender` exec hook (an unsandboxed process per artifact, a second language for policy, and nothing needs it). If a future need appears, the extension is one more `rewrite.Rule` kind, not an exec.
- Every integrator value is data, with a neutral default: retired global skills (`mcp.renamed` + `machine.retiredFiles`), renamed MCP servers (`mcp.renamed`), a role's sandbox (`codex-sandbox:`), root files mirrored into `.opencode/` (`opencode.mirrorFiles`, default none), the nested-project marker (`workbenchMarker`, default none: discovery uses `.claude/engine-sync.json` and `.git` only), the global roles source (`machine.claudeAgentsDir`, default `{Claude config dir}/agents`).

## 5. Ownership — markers, reconcile, the writer guard

- A path is engine-sync's when its first line carries a claimed marker (§ 2.4) or it is a symlink into a source root; anything else under a target root is foreign and never overwritten (`CONFLICT` with the remedy).
- Regions: `.codex/config.toml` is the Codex keeper, hand-written; engine-sync owns only its fenced `mcp_servers` region (`artifact.Region`), validates the fence (`mcpFenceProblems` moves to `target/codex/config.go`) and never appends a second fence.
- `guard`'s writer record keeps an older binary from overwriting a newer one's output; it stays as is.

## 6. Package tree

```
cmd/engine-sync/
  main.go                 process adapter into internal/cli
  version.go              release metadata
internal/
  cli/                    command table, dispatch, flags and process transport
  config/                 layers, schema walk, provenance, validation        (exists; seat's work)
  source/                 Project model, frontmatter, discovery, unresolved
  rewrite/                Rule chain, built-in rules, configured rewrites
  target/                 Target interface, Registry
  target/codex/           agents commands skills mcp orientation config review validate target
  target/opencode/        agents commands skills orientation config permission validate target
  artifact/               Artifact, Kind, Syntax, marker + legacy claims
  reconcile/              modes, states, kept twins, link and role classification
  machine/                targets.json, Codex homes, machine-scope run, variants   (renamed from global/)
  hook/                   dirty store, mark/stop/start                        (from cmd/engine-sync/hook.go logic)
  diag/                   Diagnostic, text + JSON rendering
  paths/  atomicfile/  guard/  engine/  testjail/  wrappertest/       (stay)
testdata/projects/
  plain/   input/ + golden/       the standalone fixture (P1)
  professor/ input/ + golden/     Professor's settings over a Professor-shaped tree (Rule 4)
scripts/check-tree.sh             § 9
```

Every package has a `doc.go` stating its purpose (Structure gate).

## 7. Glossary

| Concept | Canonical term | Rejected variants | Appears as |
| --- | --- | --- | --- |
| a Claude input file | source | input, original | `source.Project`, `Artifact.Source`, `--source-repo` |
| an output engine | target | backend, engine output | `target.Target`, `Registry`, `--engine` |
| one output file, link or region | artifact | generatedFile, output, twin | `artifact.Artifact` |
| a target's artifacts in one root | mirror | generated tree | docs, `MISSING` remedies |
| the ownership header | marker | header, stamp, generatedLine | `artifact.Marker` |
| a dangling-source twin kept | kept | keptTwin, unresolved twin | state `KEPT` |
| repository vs machine | scope: project, machine | global (for the scope) | `source.Scope`, `machine/` |
| a configured text replacement | rewrite | bodyReplacement, swap | `rewrites`, `rewrite.Rule` |
| settings file layers | layer: default, machine, project, env, flag | override, tier | `config show` source column |

`global` survives only as the CLI verb `engine-sync global` (pfm's contract) and as Claude's own "global commands" phrase; the scope's name is `machine`.

## 8. Façades

| Concern | Module | Primitive it hides |
| --- | --- | --- |
| file write | `atomicfile` | `os.WriteFile`, `os.Rename`, `os.Symlink` outside it |
| locations and env | `paths` | `os.Getenv`, `os.UserHomeDir`, `os.LookupEnv` outside it and `config`'s env layer |
| subprocess | `paths.Git` (or a `run` façade if a second command appears) | `exec.Command` outside it (today `internal/hook/hook.go` runs `git rev-parse` inline ×5) |
| test scratch root | `testjail` | `t.TempDir()` and `os.MkdirTemp` in tests outside it |
| Claude file parsing | `source` | YAML/frontmatter parsing anywhere else |
| marker text | `artifact` | the string `Generated by` anywhere else |

## 9. Gates

`scripts/check-tree.sh` implements `/quality:llm-codebase` § Mechanical gates for this tree (the skeleton in that command, adapted), baselines in `scripts/baselines/`, three-state lines `CHECK <id> PASS|FAIL|ERROR`, exit 0/1/2, `--measure` shrink-only. It has its own tests (`scripts/check-tree_test.sh`), each failure path watched failing first. CI runs it after vet and before tests; the gate chain is `gofmt -l` → `go vet` → `staticcheck` (pinned, versions file) → `check-tree.sh` → `go test ./...` → both `claude plugin validate --strict` → leak gate.

| Check id | Class | Asserts | Broken state reports |
| --- | --- | --- | --- |
| size-src / size-test | Size | non-test `.go` ≤ 500 lines, test ≤ 900, slack 10 | FAIL naming file and count |
| size-cmd | Size | `cmd/engine-sync` non-test total ≤ 900 lines | FAIL with the total |
| negation-dirs | Structure | no `util*`, `helpers`, `common`, `misc`, `shared` dir | FAIL naming it |
| doc-go | Structure | every package has a `// Package` doc | FAIL naming the package |
| test-mirror | Structure | every non-test `.go` but `doc.go` has a same-stem `_test.go` or is baselined | FAIL naming the source |
| testjail | Structure | no `t.TempDir`/`os.MkdirTemp` in tests outside `testjail` | FAIL per file |
| dup-functions | Duplicates | one top-level function name per name across files (case-folded; `main`, `init`, `TestMain` excluded) | FAIL naming name and files |
| facade-write / facade-env / facade-exec / facade-marker / facade-frontmatter | Façade | the § 8 primitive outside its module, per file | FAIL per file over baseline |
| spelling-professor | Spelling | `professor\|pfm\|gitter\|\.professor` in non-test `.go` and in `testdata/projects/plain/golden/**` = 0 | FAIL per file |
| spelling-states | Spelling | reconcile state words only from `reconcile.State` (no string literal `"STALE"` outside it) | FAIL per file |
| registry-parity | Registry | every command in `main.go`'s table appears in README's Commands block and `docs/interface.md`, and vice versa; `docs/engine-sync.schema.json` equals `config schema` | FAIL naming the drift |
| orientation-truth | Orientation | every path, package and env var `CLAUDE.md` and `docs/*.md` cite exists; a cited `ENGINE_SYNC_*` is read by non-test code | FAIL naming the pointer |
| publication | Publication | the leak gate over changed files, count scanned beside the verdict | ERROR with no terms file |

Baselines start at today's measure after the migration; the target state is every list empty and every count zero except `size-*` entries named in § 12.

## 10. Verification

Project goldens cover neutral standalone use and configured compatibility. Target rendering tests freeze source models and remove their source tree before rendering. Reconciliation tests cover foreign ownership, kept outputs, keeper regions and read-only modes. Machine tests cover account fanout, variants, retirement and hook lifecycle. The gate chain in § 9 checks the compiler tree and public command/schema registries.

## 11. Acceptance

The tree holds:

- A1. The § 6 tree exists, each package with `doc.go`.
- A2. `scripts/check-tree.sh` prints only PASS lines and exits 0; its tests (`scripts/check-tree_test.sh`) plant a violation for each check and watch it print FAIL.
- A3. Golden fixtures: `plain` contains no Professor term (also asserted by `spelling-professor`); `professor` equals the base binary's output except marker lines, byte for byte, for project and jail-home artifacts.
- A4. `engine-sync config show` on Professor's settings names every key's layer; `config validate` on a planted unknown key exits 1 naming file and key; `config schema` equals the tracked schema file.
- A5. pfm's contract holds: every command, flag, exit code and JSON field in today's `docs/interface.md` still works (`codex build|check`, `opencode build|check|doctor`, `build|check|doctor`, `global …`, `hook claude …`); additions are listed in `docs/interface.md`, removals none.

## 12. Open rulings decided here

- Size ceilings: 500 source / 900 test (no file is baselined over them). Alternative 800/1200 — rejected: it would bless both compiler files.
- Flow prefixes on package names: rejected for Go (`s1_` package names break Go naming and lint); the flow order is § 1's diagram and `docs/architecture.md`'s first table instead.
- `staticcheck` joins the chain, pinned; `golangci-lint` rejected for now (a second configuration surface for one module); changed-lines mode not needed on a tree this size — the whole tree is clean or baselined.
- `extends` between settings files: rejected — two layers (machine, project) cover every measured need; a profile chain adds cycle and precedence rules nobody uses.
- Regex rewrites: literal only; a regex form is added the day a real rewrite needs one.
