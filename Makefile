.PHONY: all clean znnd devnet-keys devnet-up devnet-down \
	devnet-sync-up devnet-sync-logs devnet-sync-status devnet-sync-stop devnet-sync-reset

GO ?= latest

ifeq ($(OS),Windows_NT) 
    detected_OS := Windows
else
    detected_OS := $(shell sh -c 'uname 2>/dev/null || echo Unknown')
endif

ifeq ($(detected_OS),Windows)
    EXECUTABLE=libznn.dll
endif
ifeq ($(detected_OS),Darwin)
    EXECUTABLE=libznn.dylib
endif
ifeq ($(detected_OS),Linux)
    EXECUTABLE=libznn.so
endif

SERVERMAIN = $(shell pwd)/cmd/znnd
LIBMAIN = $(shell pwd)/cmd/libznn
BUILDDIR = $(shell pwd)/build
GIT_COMMIT=$(shell git rev-parse HEAD)
GIT_COMMIT_FILE=$(shell pwd)/metadata/git_commit.go

$(EXECUTABLE):
	go build -o $(BUILDDIR)/$(EXECUTABLE) -buildmode=c-shared -tags libznn $(LIBMAIN)

libznn: $(EXECUTABLE) ## Build binaries
	@echo "Build libznn done."

znnd:
	go build -o $(BUILDDIR)/znnd $(SERVERMAIN)
	@echo "Build znnd done."
	@echo "Run \"$(BUILDDIR)/znnd\" to start znnd."

clean:
	rm -r $(BUILDDIR)/

all: znnd libznn

devnet-keys:
	go run ./cmd/devnet-keygen $(if $(FORCE),--force,)

devnet-up:
	docker compose up -d --build

# --profile sync so a previously activated sync node is torn down too;
# without it, `down` skips the profiled syncnode, which keeps the bridge
# network alive and leaves the sync node running across the next `devnet-up`.
devnet-down:
	docker compose --profile sync down -v

# --- Late-joiner sync node (docker-compose "sync" profile) -------------------
# Activate a cold node and watch it discover peers and catch up to the frontier.
# Requires the devnet to already be running (`make devnet-up`).

# Start (or resume) the sync node. First start syncs from genesis; a start after
# `devnet-sync-stop` resumes from where it left off (no wipe).
devnet-sync-up:
	docker compose --profile sync up -d syncnode
	@echo "sync node up. Follow it with: make devnet-sync-logs"
	@echo "Or poll progress with:        make devnet-sync-status"

# Follow the sync node logs (peer discovery + block download).
devnet-sync-logs:
	docker compose --profile sync logs -f syncnode

# Poll catch-up progress over the host-exposed RPC port (35999).
# state: 0=unknown 1=syncing 2=done 3=not-enough-peers.
devnet-sync-status:
	@curl -sX POST http://localhost:35999 \
		-H 'Content-Type: application/json' \
		-d '{"jsonrpc":"2.0","id":1,"method":"stats.syncInfo","params":[]}'
	@echo

# Stop without wiping state (the next `devnet-sync-up` resumes).
devnet-sync-stop:
	docker compose --profile sync stop syncnode

# Wipe the sync node only (remove its container + writable layer) so the next
# `devnet-sync-up` re-syncs from genesis. Leaves the rest of the devnet running.
devnet-sync-reset:
	docker compose --profile sync rm -fs syncnode
	@echo 'sync node wiped. "make devnet-sync-up" will re-sync from genesis.'
