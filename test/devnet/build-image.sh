#!/bin/sh
# Build the devnet image (go-zenon-devnet:latest) from the current checkout without
# running the multi-stage Dockerfile build inside the docker VM.
#
# `make devnet-up` is the normal path. Use this script when the docker VM is short on
# disk or when you want the binary built with a specific Go toolchain: the binary is
# compiled in a Go container (cgo is required by secp256k1) with the source mounted
# read-only, then copied into a small alpine image that matches the final stage of
# the repository Dockerfile.
#
#   GOIMG=golang:1.24.1-alpine test/devnet/build-image.sh
#   FAST=1 test/devnet/build-image.sh     # the test build, image go-zenon-devnet-fast:latest
#
# FAST=1 adds the files in test/devnet/fast/ through a Go build overlay: a short
# epoch, short locks and a bridge administrator whose key the driver holds. The
# version string of that binary ends in "+fast-constants". It is for procedure P11
# only; it is not the binary under test for anything else.
#
# The source directory must be mountable by the docker VM (with Colima: under $HOME).
set -eu
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GOIMG="${GOIMG:-golang:1.24.1-alpine}"
FAST="${FAST:-0}"
if [ "$FAST" = 1 ]; then
  IMAGE="${IMAGE:-go-zenon-devnet-fast:latest}"; OUT="$ROOT/build/linux-fast"
else
  IMAGE="${IMAGE:-go-zenon-devnet:latest}"; OUT="$ROOT/build/linux"
fi
CTX="$(mktemp -d)"
trap 'rm -rf "$CTX"' EXIT
PKG=github.com/zenon-network/go-zenon/metadata
VERSION="$(git -C "$ROOT" describe --tags --always)"
COMMIT="$(git -C "$ROOT" rev-parse HEAD)"
mkdir -p "$OUT"
OVERLAY=""
if [ "$FAST" = 1 ]; then
  VERSION="$VERSION+fast-constants"
  "$ROOT/test/devnet/fast/overlay.sh" "$ROOT" /src > "$OUT/overlay.json"
  OVERLAY="-overlay /out/overlay.json"
fi

docker run --rm \
  -v "$ROOT":/src:ro -v "$OUT":/out \
  -v go-zenon-test-gomod:/go/pkg/mod -v go-zenon-test-gocache:/root/.cache/go-build \
  -w /src -e CGO_ENABLED=1 -e GOWORK=off -e GOFLAGS=-buildvcs=false \
  "$GOIMG" sh -ec "apk add --no-cache gcc musl-dev >/dev/null && go build $OVERLAY -o /out/znnd -ldflags '-X $PKG.Version=$VERSION -X $PKG.GitCommit=$COMMIT' ./cmd/znnd && go version"

cp "$OUT/znnd" "$CTX/znnd"
cp -R "$ROOT/docker/devnet" "$CTX/devnet"
cat > "$CTX/Dockerfile" <<'DOCKER'
FROM alpine:latest
WORKDIR /root
RUN mkdir -p /root/.znn
COPY znnd /usr/local/bin/znnd
COPY devnet /devnet
COPY devnet/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh /usr/local/bin/znnd
EXPOSE 35995 35997 35998
ENTRYPOINT ["/entrypoint.sh"]
DOCKER
docker build -q -t "$IMAGE" "$CTX"
echo "image $IMAGE built from $COMMIT"
shasum -a 256 "$OUT/znnd" 2>/dev/null || sha256sum "$OUT/znnd"
docker run --rm --entrypoint znnd "$IMAGE" version | grep -E 'Version|Go Version|Commit'
