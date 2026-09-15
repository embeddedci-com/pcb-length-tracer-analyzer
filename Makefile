# pcb-trace-length-analyzer -- length matching for KiCad boards.
#
# `make report` and `make tune` run against the demo board; BOARD= points them
# at any other. Nothing here writes to demo-pcb/.

BIN       := bin/pcb-trace-length-analyzer
SERVERBIN := bin/pcb-trace-length-analyzer-server
ENGINEBIN := bin/pcb-trace-length-analyzer-engine
BOARD     ?= demo-pcb/ai-vision.kicad_pcb
WORK      := out
PORT      ?= 8091
GOFLAGS   ?=

.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo "pcb-trace-length-analyzer"
	@echo
	@echo "  make build          build $(BIN)"
	@echo "  make test           unit tests (fast, no KiCad needed)"
	@echo "  make test-all       unit tests plus the end-to-end run against kicad-cli"
	@echo "  make report         analyse the demo board, change nothing"
	@echo "  make tune           analyse, tune a copy into $(WORK)/, verify with kicad-cli"
	@echo "  make verify         compare $(WORK)/before vs $(WORK)/after with kicad-cli"
	@echo "  make fixtures       regenerate testdata/golden-lengths.json from kicad-cli"
	@echo "  make lint           gofmt check and go vet"
	@echo "  make clean"
	@echo
	@echo "Milestone 2, the web front end:"
	@echo "  make serve          build the webapp and serve it with the API on :$(PORT)"
	@echo "  make webapp         install, typecheck, test and build webapp/"
	@echo "  make webapp-dev     Vite dev server, proxying /api to the control plane"
	@echo "  make webapp-test    webapp typecheck and unit tests"
	@echo
	@echo "The KiCad plugin (kicad-plugin/):"
	@echo "  make plugin         build the engine and the report into kicad-plugin/"
	@echo "  make plugin-test    the plugin's Python tests (PLUGIN_PY= a Python with PySide6 + kicad-python)"
	@echo "  make plugin-install link kicad-plugin/ into KiCad's plugins folder (KICAD_PLUGINS=...)"
	@echo "  make plugin-dist    release zips for every platform into dist/"
	@echo "  make pcm-release VERSION=x.y.z   package for KiCad's Plugin and Content Manager into PCM_REPO"
	@echo
	@echo "  BOARD=path/to/board.kicad_pcb overrides the board (default: the demo)"
	@echo "                      see demo-pcb/README.md before replacing the fixture"

.PHONY: build
build:
	@mkdir -p bin
	go build $(GOFLAGS) -o $(BIN) ./cmd/pcb-trace-length-analyzer
	go build $(GOFLAGS) -o $(SERVERBIN) ./cmd/pcb-trace-length-analyzer-server
	go build $(GOFLAGS) -o $(ENGINEBIN) ./cmd/pcb-trace-length-analyzer-engine

.PHONY: test
test:
	go test $(GOFLAGS) ./...
	@$(MAKE) --no-print-directory webapp-test

# The end-to-end test needs kicad-cli and takes about half a minute: it tunes
# the demo board and then asks KiCad whether the result is sound.
.PHONY: test-all
test-all:
	go test $(GOFLAGS) ./...
	go test $(GOFLAGS) -tags integration -v .
	@$(MAKE) --no-print-directory webapp-test
	@$(MAKE) --no-print-directory plugin-test-if-possible

# The plugin shares the site's frontend (webapp/src) and its server package, so
# nothing is copied that could fall behind. What can break is the seam: the
# Python client's idea of the endpoints, the scheme handler, and the report
# rendering inside QtWebEngine. plugin-test checks all three against the real
# engine, and runs as part of test-all whenever PLUGIN_PY has what it needs.
.PHONY: plugin-test-if-possible
plugin-test-if-possible:
	@if $(PLUGIN_PY) -c "import pytest, kipy, PySide6.QtWebEngineCore" >/dev/null 2>&1; then \
	  $(MAKE) --no-print-directory plugin-test; \
	else \
	  echo "SKIPPED plugin tests: PLUGIN_PY=$(PLUGIN_PY) lacks pytest, kicad-python or PySide6"; \
	  echo "  e.g. PLUGIN_PY=~/Library/Caches/kicad/10.0/python-environments/com.embeddedci.pcb-trace-length-analyzer/bin/python (after pip install pytest)"; \
	fi

.PHONY: lint
lint:
	@out=$$(gofmt -l . 2>/dev/null); \
	  if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	go vet ./...
	go vet -tags integration ./...

.PHONY: report
report: build
	$(BIN) $(BOARD)

# Tune a copy. The project and rules files travel with it, because a board
# without them has no net classes and KiCad's DRC would be meaningless.
.PHONY: tune
tune: build
	@mkdir -p $(WORK)
	@base=$$(basename $(BOARD) .kicad_pcb); dir=$$(dirname $(BOARD)); \
	  cp $(BOARD) $(WORK)/before.kicad_pcb; \
	  for ext in kicad_pro kicad_dru; do \
	    if [ -f "$$dir/$$base.$$ext" ]; then \
	      cp "$$dir/$$base.$$ext" "$(WORK)/before.$$ext"; \
	      cp "$$dir/$$base.$$ext" "$(WORK)/after.$$ext"; \
	    fi; \
	  done
	$(BIN) -apply -out $(WORK)/after.kicad_pcb $(WORK)/before.kicad_pcb
	@$(MAKE) --no-print-directory verify

.PHONY: verify
verify:
	@if [ ! -f $(WORK)/after.kicad_pcb ]; then echo "run 'make tune' first"; exit 1; fi
	./scripts/verify-with-kicad.sh $(WORK)/before.kicad_pcb $(WORK)/after.kicad_pcb

.PHONY: fixtures
fixtures:
	./scripts/golden-lengths.sh $(BOARD) testdata/golden-lengths.json

# ---- milestone 2: the web front end ----

WEBAPP_DEPS := webapp/node_modules/.package-lock.json

$(WEBAPP_DEPS): webapp/package.json
	npm --prefix webapp install --no-audit --no-fund
	@touch $@

.PHONY: webapp-deps
webapp-deps: $(WEBAPP_DEPS)

.PHONY: webapp-test
webapp-test: webapp-deps
	npm --prefix webapp run typecheck
	npm --prefix webapp run test

.PHONY: webapp
webapp: webapp-deps
	npm --prefix webapp run build

.PHONY: webapp-dev
webapp-dev: webapp-deps
	@echo "start the control plane in another shell first:  make serve-api"
	npm --prefix webapp run dev

# Serve the built front end and the API from one origin, which is how it will
# run once it is mounted into embeddedci-server.
.PHONY: serve
serve: build webapp
	$(SERVERBIN) -addr 127.0.0.1:$(PORT) -webapp webapp/dist

.PHONY: serve-api
serve-api: build
	$(SERVERBIN) -addr 127.0.0.1:$(PORT)

# ---- the KiCad plugin ----
#
# kicad-plugin/ is the plugin folder exactly as KiCad loads it. The engine and
# the built report are generated into it (and gitignored), so the folder can be
# linked into KiCad and used in place while it is being worked on.

PLUGIN      := kicad-plugin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PLUGIN_PY   ?= python3
HOST_TAG    := $(shell go env GOOS)-$(shell go env GOARCH)
RELEASE_TAGS := darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 windows-amd64

ifeq ($(shell uname -s),Darwin)
KICAD_PLUGINS ?= $(HOME)/Documents/KiCad/10.0/plugins
else
KICAD_PLUGINS ?= $(HOME)/.local/share/kicad/10.0/plugins
endif

.PHONY: plugin
plugin: webapp-deps
	@mkdir -p $(PLUGIN)/bin/$(HOST_TAG)
	go build $(GOFLAGS) -ldflags "-s -w -X main.version=$(VERSION)" \
	  -o $(PLUGIN)/bin/$(HOST_TAG)/pcb-trace-length-analyzer-engine$(if $(findstring windows,$(HOST_TAG)),.exe,) \
	  ./cmd/pcb-trace-length-analyzer-engine
	npm --prefix webapp run build:kicad

.PHONY: plugin-test
plugin-test: plugin
	cd $(PLUGIN) && $(PLUGIN_PY) -m pytest -q tests

# A link, not a copy: edits to the Python take effect the next time the button
# is pressed. Restart KiCad (or Preferences -> Plugins -> Reload) after the
# first install so it finds the new folder.
.PHONY: plugin-install
plugin-install: plugin
	@mkdir -p "$(KICAD_PLUGINS)"
	@if [ -e "$(KICAD_PLUGINS)/pcb-trace-length-analyzer" ] && [ ! -L "$(KICAD_PLUGINS)/pcb-trace-length-analyzer" ]; then \
	  echo "$(KICAD_PLUGINS)/pcb-trace-length-analyzer exists and is not a link; remove it first"; exit 1; fi
	ln -sfn "$(CURDIR)/$(PLUGIN)" "$(KICAD_PLUGINS)/pcb-trace-length-analyzer"
	@echo "linked into $(KICAD_PLUGINS); restart KiCad, and enable Preferences -> Plugins -> KiCad API"

# One zip per platform, each holding only that platform's engine. Unzip into
# KiCad's plugins folder.
.PHONY: plugin-dist
plugin-dist: webapp-deps
	@rm -rf dist && mkdir -p dist
	npm --prefix webapp run build:kicad
	@set -e; for tag in $(RELEASE_TAGS); do \
	  os=$${tag%-*}; arch=$${tag#*-}; ext=; [ $$os = windows ] && ext=.exe; \
	  stage=dist/stage-$$tag/pcb-trace-length-analyzer; mkdir -p $$stage/bin/$$tag; \
	  echo "engine $$tag"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" \
	    -o $$stage/bin/$$tag/pcb-trace-length-analyzer-engine$$ext ./cmd/pcb-trace-length-analyzer-engine; \
	  cp -R $(PLUGIN)/plugin.json $(PLUGIN)/requirements.txt $(PLUGIN)/README.md $(PLUGIN)/analyze.py \
	    $(PLUGIN)/net_length.py $(PLUGIN)/icons $(PLUGIN)/web $$stage/; \
	  mkdir -p $$stage/trace_length_analyzer && cp $(PLUGIN)/trace_length_analyzer/*.py $$stage/trace_length_analyzer/; \
	  (cd dist/stage-$$tag && zip -qr ../pcb-trace-length-analyzer-$(VERSION)-$$tag.zip pcb-trace-length-analyzer); \
	  rm -rf dist/stage-$$tag; \
	done
	@ls -lh dist

# ---- the Plugin and Content Manager repository ----
#
# The repository is its own public GitHub repo: repository.json, packages.json
# and resources.zip on its main branch, and each version's archive attached to
# a release there. This target builds the archive and updates a checkout of
# that repo; publishing is committing that checkout and uploading the archive
# (the target prints how).
#
# Users add https://raw.githubusercontent.com/$(PCM_GITHUB)/main/repository.json
# under Plugin and Content Manager -> Manage repositories.

PCM_GITHUB  ?= embeddedci-com/kicad-plugins
PCM_REPO    ?= ../kicad-plugins
PCM_STATUS  ?= testing
PCM_TAG      = pcb-trace-length-analyzer-v$(VERSION)
PCM_ZIP      = pcb-trace-length-analyzer-$(VERSION).zip
PCM_RAW_URL ?= https://raw.githubusercontent.com/$(PCM_GITHUB)/main
PCM_DL_URL  ?= https://github.com/$(PCM_GITHUB)/releases/download/$(PCM_TAG)/$(PCM_ZIP)

.PHONY: pcm-engines
pcm-engines:
	@set -e; for tag in $(RELEASE_TAGS); do \
	  os=$${tag%-*}; arch=$${tag#*-}; ext=; [ $$os = windows ] && ext=.exe; \
	  mkdir -p $(PLUGIN)/bin/$$tag; echo "engine $$tag"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" \
	    -o $(PLUGIN)/bin/$$tag/pcb-trace-length-analyzer-engine$$ext ./cmd/pcb-trace-length-analyzer-engine; \
	done

.PHONY: pcm-release
pcm-release: webapp-deps
	@if [ -z "$(VERSION)" ]; then echo "usage: make pcm-release VERSION=0.1.0 [PCM_STATUS=stable] [PCM_REPO=../kicad-plugins]"; exit 1; fi
	@if ! grep -q '__version__ = "$(VERSION)"' $(PLUGIN)/trace_length_analyzer/__init__.py; then \
	  echo "trace_length_analyzer/__init__.py does not say __version__ = \"$(VERSION)\"; bump it first"; exit 1; fi
	npm --prefix webapp run build:kicad
	@$(MAKE) --no-print-directory pcm-engines VERSION=$(VERSION)
	python3 $(PLUGIN)/scripts/pcm_release.py --version $(VERSION) --status $(PCM_STATUS) \
	  --repo $(PCM_REPO) --download-url $(PCM_DL_URL) --repo-url $(PCM_RAW_URL)
	@echo
	@echo "to publish:"
	@echo "  gh release create $(PCM_TAG) dist/pcm/$(PCM_ZIP) -R $(PCM_GITHUB) --title 'PCB Trace Length Analyzer $(VERSION)'"
	@echo "  git -C $(PCM_REPO) add -A && git -C $(PCM_REPO) commit -m 'PCB Trace Length Analyzer $(VERSION)' && git -C $(PCM_REPO) push"
	@echo "  (the release first: packages.json must not point at an archive that is not there yet)"

.PHONY: clean
clean:
	rm -rf bin $(WORK) webapp/dist dist $(PLUGIN)/bin $(PLUGIN)/web
