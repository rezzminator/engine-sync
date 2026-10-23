# The checks GitHub runs on a pull request to main (.github/workflows/release.yml),
# runnable here first: `make release-check` runs them all.
GO_VERSION := $(shell awk '/^go /{print $$2}' go.mod)
# go.mod's toolchain exactly, as setup-go gives CI; build-release.sh refuses any other.
export GOTOOLCHAIN := go$(GO_VERSION)
MANIFEST := plugins/engine-sync/.claude-plugin/plugin.json
JSON_VERSION := grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | head -n 1 | sed 's/^"version"[[:space:]]*:[[:space:]]*"//; s/"$$//'
# The runners of the release-version matrix, each with the `uname -s`/`uname -m` it builds natively.
VERSION_LEGS := ubuntu-24.04=Linux/x86_64 ubuntu-24.04-arm=Linux/aarch64 macos-15=Darwin/arm64 macos-15-intel=Darwin/x86_64

.PHONY: release-check check tree tree-public leak release-version release

release-check: check tree leak release-version release
	@echo "NOT RUN LOCALLY: public compiler tree on fork pull requests — runs only on a fork's pull request, which gets no secrets; make tree ran every check it runs"
	@native="$$(uname -s)/$$(uname -m)"; for leg in $(VERSION_LEGS); do \
	  [ "$${leg#*=}" = "$$native" ] || echo "NOT RUN LOCALLY: release-version on $${leg%%=*} — needs a $${leg#*=} runner; this host builds $$native"; \
	done

check:
	unformatted=$$(sh -c '"$$(go env GOROOT)/bin/gofmt" -l .'); \
	[ -z "$$unformatted" ] || { printf 'gofmt: not formatted:\n%s\n' "$$unformatted"; exit 1; }
	go vet ./...
	version=$$(awk '$$1 == "staticcheck" { print $$2 }' scripts/tool-versions.txt); \
	go run honnef.co/go/tools/cmd/staticcheck@"$$version" ./...
	scripts/check-tree_test.sh
	go test ./...
	claude plugin validate --strict .
	claude plugin validate --strict plugins/engine-sync

# LEAK_TERMS names the terms file (CI writes the secret to one); unset, scripts/leak-terms.txt.
tree:
	scripts/check-tree.sh

# Every tree check but publication, which needs the private terms.
tree-public:
	for baseline in scripts/baselines/*.txt; do \
	  check=$$(basename "$$baseline" .txt); \
	  [ "$$check" = publication ] || scripts/check-tree.sh --only "$$check" || exit 1; \
	done

leak:
	scripts/leak-check.sh

# The native release asset runs and prints the plugin's version.
release-version:
	out=$$(mktemp -d); trap 'rm -rf "$$out"' EXIT; \
	scripts/build-release.sh --native-only --out "$$out/release"

# The version must move past main's: release-check.sh takes main's version as its base.
release:
	git rev-parse --verify --quiet origin/main >/dev/null || { echo "release: origin/main not found; fetch it first" >&2; exit 1; }
	base=$$(git show origin/main:$(MANIFEST) 2>/dev/null | $(JSON_VERSION)); \
	scripts/release-check.sh --release "$${base:-0.0.0}"
	v=$$(cat $(MANIFEST) | $(JSON_VERSION)); \
	awk -v heading="## [$$v]" 'index($$0, heading) == 1 { found = 1; next } found && /^## \[/ { exit } found' CHANGELOG.md | grep -q '' \
	  || { echo "release: CHANGELOG.md section for $$v yields no release notes" >&2; exit 1; }
