# engine-sync architecture

## Contents

- [Pipeline](#pipeline)
- [Configuration and targets](#configuration-and-targets)
- [Ownership and concurrency](#ownership-and-concurrency)
- [Extension points](#extension-points)
- [Package responsibilities](#package-responsibilities)
- [Tree gates](#tree-gates)
- [Verification](#verification)

## Pipeline

The CLI resolves policy and destinations, snapshots Claude sources, renders each target, reconciles artifacts, then reports findings. Both project and machine scopes use `engine.Execute`; machine orchestration also retires configured obsolete outputs. The CLI coordinates machine and project runs under separate guards.

`source.Project` contains orientation files, typed agents and commands, skills, MCP servers, child projects and unresolved source links. It also freezes document and directory caches. Target renderers derive their naming rosters from that snapshot; they do not read the filesystem. Directory aliases are independent inputs, while an alias back into the active traversal ancestry is a cycle. Configured excludes prune directory components before parsing.

`target.Target` supplies Name, Roots, Render and Validate. Roots describe managed output surfaces, optional validation-only surfaces, foreign native-file inspection and the enclosing write boundary. Artifacts describe a file, link or fenced region, its source, mode and syntax. Validators inspect rendered artifacts and declared existing native surfaces; unrelated files stay outside their consumer surface.

Codex rewrites model aliases in body text and command references to skill spellings. OpenCode maps model pins in frontmatter and rewrites command references; its body text does not swap model aliases. Configured literal rewrites run after built-in transformations. Codex review translation uses optional policy for model, effort and prompt. With no review policy, a bare invocation becomes `codex review`.

Malformed source frontmatter is an error in either target. Missing command descriptions are per-file warnings. Frontmatter supports BOM, CRLF, spaced fences and quoted or flow-list tools; invalid UTF-8 and NUL fail at source entry. Every source read requires a regular file and has an 8 MiB bound. FIFO and device inputs are rejected before opening. Ownership headers have a 4 KiB bound.

## Configuration and targets

The typed resolver merges defaults, machine policy, project policy, environment and flags. Maps merge, lists replace, and ordered permission tools replace their own rule sets. A lower bare permission survives as a leading wildcard fallback. Every effective key has provenance. Unknown or duplicate JSON keys, malformed templates and invalid paths are named errors. The schema is generated from the Go types and checked against `docs/engine-sync.schema.json`; the key inventory is checked against the public tables.

Machine-only stores, sources, variants, retirements and destinations are forbidden in project files. Explicit source repositories use the configured agents directory. Additional project command directories and marker-source directories are policy data with neutral defaults. Product-specific adapters, preambles, model maps and access rules belong to integrator policy.

The shared machine resolver selects explicit roster flags, CODEX_HOME, targets.json, explicit machine.codexHomes, then defaults. Explicit empty lists remain empty. An explicit roster is a recovery path past malformed targets.json. Doctor, global, agents and SessionStart use the same resolver. Machine command output depends only on machine inputs and policy; project policy affects only project artifacts. A workbench suppresses home writes.

Project roots are physical paths. Discovery stops at the nearest configured marker, Git root or regular orientation file. HOME and every ancestor of HOME are refused. Config show and validate use the same resolver as build.

## Ownership and concurrency

`artifact.Marker` renders every comment wrapper and rebuild hint. `artifact.Claims` recognizes complete current or historical producer headers in the ownership-header position. A partial phrase or a body quotation is not ownership. A link is owned only when its creation receipt matches its exact target; pointing into a source directory does not establish ownership.

Reconciliation preserves foreign files, roles, links and directory children. It removes only owned files or recorded links, then empty directories. Plan names the foreign children that build will preserve. Any failed ownership read is a finding. Concurrent scratch files are excluded from garbage collection. Desired nested roots and folded, NFC-normalized output names cannot overwrite each other; collisions name both producers.

Managed directories and parents cannot redirect output through a symlink. An explicitly selected Claude configuration directory is resolved once. Keeper regions preserve their file mode and user-owned content. A keeper link is retained and its target edited only within the declared managed root. JSONC parsing accepts line/block comments and trailing commas while preserving user keys outside the owned schema, permission and MCP fields; malformed JSONC remains untouched.

CLI writers sort and deduplicate canonical guard keys, hold their locks, consult version records, execute, then record every admitted attempt, including partial failures. Shared home command destinations have machine guards separate from project guards. Check and doctor consult those records without writing. Ordinary CLI writers wait; hooks bound each wait to 250 ms and answer BUSY as a non-blocking systemMessage.

The dirty flag remains durable while Stop stages run. A short dirty-store lock protects mark and successful cleanup; cleanup verifies the same inode, timestamp and size before removing it. A concurrent mark or process death therefore preserves dirtiness. Stop stage failure blocks once, with subsequent active Stop failures reporting a systemMessage. Root, state and output errors remain visible.

Atomic publication syncs file contents and the containing directory. A failure after publication is reported even though the new file may already be visible. Existing parent modes are preserved; newly created parents use 0755.

## Extension points

`target.Registry` is the dispatch authority. A new target requires its target package and registry entry; its command aliases, user documentation and native consumer tests must also be supplied when desired. Engine descriptors derive from target names instead of a parallel registry. Shared discovery, naming validation and reconciliation apply automatically.

Adapters and preambles use Name, Engine, Source and Root template fields. Engine-prefixed frontmatter overrides are generic. The legacy dollar-name spelling remains literal in root adapters and is converted only in preambles. Native permissions, model maps, mirror-file policy, MCP knowledge, review policy and literal rewrites provide the integration surface; there is no executable extension hook.

Canonical public terms are source, target, artifact, mirror, marker, project scope, machine scope, rewrite and layer. Existing API and CLI compatibility names remain; tree gates check specified primitive and literal boundaries, not every prose synonym.

## Package responsibilities

| Package | Responsibility |
| --- | --- |
| cli | Commands, transport, policy caching and guarded orchestration |
| config | Layers, provenance, typed target roster and schema |
| source | Bounded reads, frontmatter, discovery, variants and naming |
| rewrite | Shared text transformations and literal rules |
| target | Registry and target interface |
| target/codex, target/opencode | Native renderers and validators |
| artifact | Artifact shapes and ownership-marker syntax |
| engine | Shared rendering, validation and reconciliation pipeline |
| reconcile | Filesystem plan, ownership, kept outputs and selective cleanup |
| machine | Machine plan/apply, retirements and doctor rows |
| hook | Payload, scope, dirty store and lifecycle stages |
| paths, atomicfile, guard | Path identity, bounded IO, publication and version guards |
| diag | Diagnostic shapes |
| testjail, wrappertest | Isolated tests and wrapper fixtures |

## Tree gates

`scripts/check-tree.sh` reports PASS, FAIL or ERROR per check. A missing producer, malformed input, missing baseline, empty source enumeration or empty leak scan is ERROR. Baselines compare against the Git base in ordinary runs as well as measure mode; measure refuses growth and never rewrites a failed input.

| Check | Enforced surface |
| --- | --- |
| size-src, size-test, size-cmd | 510 source lines, 910 test lines, 900 command-package lines; existing ceilings may only shrink |
| negation-dirs | Rejected generic directory names |
| doc-go, test-mirror, testjail | Package documentation, source test twins, approved test scratch |
| dup-functions | Case-folded top-level function names across the tree |
| facade-write, facade-env, facade-exec | Declared low-level primitive boundaries |
| facade-marker, facade-frontmatter | Marker and Claude parser boundaries |
| spelling-professor | Operative product policy/layout in Go and shipped text, plus neutral goldens; exact historical decoder lines and fixture/check producers are exceptions |
| spelling-states | Literal diagnostic states outside the reconciler |
| registry-parity | Command/key tables, schema producer, and development prerelease strictly newer than the plugin manifest |
| orientation-truth | Literal repository pointers and declared engine-sync environment variables, including the extensionless wrapper |
| publication | A nonempty private denylist and a successful nonempty leak scan |

The lexical gates do not prove render purity, every caller relationship or the complete glossary. Behavioral tests and review provide those checks. Runtime filename patterns, exact migration allowlists and baselines are visible in `scripts/check-tree.py` and its tests.

## Verification

The full gate runs Go tests, tree-gate planted tests, the tree gate with leak terms, vet, gofmt, pinned staticcheck, both strict plugin validations and the leak gate. Every probe redirects HOME and state into scratch. Fixtures are invented; the plain family checks neutral defaults and the integration family carries explicit policy. Byte changes from the prior release are listed in CHANGELOG.

Release assets are stamped from the plugin manifest. Build executes its native asset to verify the version stamp; CI executes each platform/architecture in its native runner. Development stays a strictly newer prerelease, so a released binary cannot silently take over its roots. Compatibility comparisons normalize corresponding jail roots and ownership/rebuild lines; deliberate changes are separately classified and named.
