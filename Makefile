.PHONY: all clean znnd devnet-keys devnet-up devnet-down testnet-ptlc testnet-ptlc-keep ptlc-fuzz test-ptlc-fuzz

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
LOCAL_GOCACHE ?= $(shell pwd)/.gocache
TESTNET_GO_TEST_FLAGS ?= -timeout 25m
TESTNET_RESULTS_DIR ?= $(shell pwd)/test-results/ptlc
PTLC_FUZZ_RESULTS_DIR ?= $(shell pwd)/test-results/ptlc-fuzz
PTLC_FUZZ_UNIT_TIMEOUT ?= 120s
PTLC_FUZZ_TIME ?= 5s
PTLC_FUZZ_TIMEOUT ?= 45s

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

devnet-down:
	docker compose down -v

testnet-ptlc:
	sh -c 'set -e; trap "docker compose down -v" EXIT; docker compose down -v; docker compose up -d --build; PTLC_TESTNET_RPC="$${PTLC_TESTNET_RPC:-http://localhost:35997}" GOCACHE="$(LOCAL_GOCACHE)" TESTNET_GO_TEST_FLAGS="$(TESTNET_GO_TEST_FLAGS)" TESTNET_RESULTS_DIR="$(TESTNET_RESULTS_DIR)" bash testnet/ptlc/run-suite.sh'

testnet-ptlc-keep:
	docker compose up -d --build
	PTLC_TESTNET_RPC="$${PTLC_TESTNET_RPC:-http://localhost:35997}" GOCACHE="$(LOCAL_GOCACHE)" TESTNET_GO_TEST_FLAGS="$(TESTNET_GO_TEST_FLAGS)" TESTNET_RESULTS_DIR="$(TESTNET_RESULTS_DIR)" bash testnet/ptlc/run-suite.sh

ptlc-fuzz:
	GOCACHE="$(LOCAL_GOCACHE)" PTLC_FUZZ_RESULTS_DIR="$(PTLC_FUZZ_RESULTS_DIR)" PTLC_FUZZ_UNIT_TIMEOUT="$(PTLC_FUZZ_UNIT_TIMEOUT)" PTLC_FUZZ_TIME="$(PTLC_FUZZ_TIME)" PTLC_FUZZ_TIMEOUT="$(PTLC_FUZZ_TIMEOUT)" bash testnet/ptlc/run-fuzz-suite.sh

test-ptlc-fuzz: ptlc-fuzz
