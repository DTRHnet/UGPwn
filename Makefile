# echoheistv2 — private research tooling.
#
# `make` runs the full local gate: formatting, vet, tests, then a binary stamped
# with version/commit/build date so a given artifact can be traced to a commit.

VERSION ?= 0.2.0
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BIN     := echoheistv2
DISTDIR := dist
NAME    := $(BIN)-$(VERSION)-linux-amd64
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(DATE)

.PHONY: all build web test race vet fmt check deps run dashboard dist smoke clean

all: fmt vet test build

web: ## build the Preact dashboard into browser/dashboard/dist (embedded in the binary)
	npm --prefix web run build

build: web ## build the binary with version metadata
	cd browser && go build -trimpath -ldflags '$(LDFLAGS)' -o ../$(BIN) .

test: ## hermetic unit tests (no network, no database)
	cd browser && go test ./...

race: ## unit tests under the race detector
	cd browser && go test -race -count=1 ./...

vet:
	cd browser && go vet ./...

fmt:
	cd browser && gofmt -w .

check: fmt vet test ## what CI runs

deps:
	npm install
	npm --prefix web install

run: build
	./start.sh

dashboard: build
	./start-dashboard.sh

dist: build ## versioned tarball + SHA256SUMS, no node_modules/tabs/logs
	rm -rf "$(DISTDIR)/$(NAME)" "$(DISTDIR)/$(NAME).tar.gz" "$(DISTDIR)/SHA256SUMS"
	mkdir -p "$(DISTDIR)/$(NAME)/browser"
	cp $(BIN) start.sh start-dashboard.sh package.json README.md SECURITY.md "$(DISTDIR)/$(NAME)/"
	cp download-worker.mjs "$(DISTDIR)/$(NAME)/"
	cp browser/download-worker.mjs "$(DISTDIR)/$(NAME)/browser/"
	cd "$(DISTDIR)" && tar -czf "$(NAME).tar.gz" "$(NAME)"
	cd "$(DISTDIR)" && sha256sum "$(NAME).tar.gz" > SHA256SUMS && cat SHA256SUMS

smoke: build ## contract smoke test of the running dashboard
	./scripts/smoke.sh ./$(BIN)

clean:
	rm -rf $(BIN) $(DISTDIR) browser/echoheistv2 browser/dashboard/dist
