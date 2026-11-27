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
#   GOIMG=golang:1.27.4-alpine test/devnet/build-image.sh
#   FAST=1 test/devnet/build-image.sh     # the test build, image go-zenon-devnet-fast:latest
#   OLDNODE=1 test/devnet/build-image.sh  # the S-4/X-4 oldnode build, image go-zenon-devnet-noref:latest
#
# FAST=1 adds the files in test/devnet/fast/ through a Go build overlay: a short
# epoch, short locks and a bridge administrator whose key the driver holds. The
# version string of that binary ends in "+fast-constants". It is for procedure P11
# only; it is not the binary under test for anything else.
#
# OLDNODE=1 removes EmbeddedRefundSpork from ImplementedSporksMap by applying
# test/devnet/oldnode/embedded-refund-removal.patch to a scratch copy of the
# source inside the build container (the checkout itself stays untouched and
# is mounted read-only). The result is the binary used by the S-4/X-4 case: a
# node that cannot pass the EmbeddedRefundSpork enforcement height. It differs
# from the stock binary only by that one map entry.
#
# The source directory must be mountable by the docker VM (with Colima: under $HOME).
set -eu
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GOIMG="${GOIMG:-golang:1.27.4-alpine}"
FAST="${FAST:-0}"
OLDNODE="${OLDNODE:-0}"
if [ "$FAST" = 1 ] && [ "$OLDNODE" = 1 ]; then
  # The oldnode branch below builds without the fast overlay but would
  # write into the fast output directory with a fast version suffix.
  echo "build-image: FAST=1 and OLDNODE=1 together are not supported" >&2
  exit 2
fi
if [ "$FAST" = 1 ]; then
  IMAGE="${IMAGE:-go-zenon-devnet-fast:latest}"; OUT="$ROOT/build/linux-fast"
elif [ "$OLDNODE" = 1 ]; then
  IMAGE="${IMAGE:-go-zenon-devnet-noref:latest}"; OUT="$ROOT/build/linux-oldnode"
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

if [ "$OLDNODE" = 1 ]; then
  # Apply the one-line spork removal to a scratch copy of the source inside
  # the container; the mounted checkout stays read-only and untouched.
  docker run --rm \
    -v "$ROOT":/src:ro -v "$OUT":/out \
    -v go-zenon-test-gomod:/go/pkg/mod -v go-zenon-test-gocache:/root/.cache/go-build \
    -w /src -e CGO_ENABLED=1 -e GOWORK=off -e GOFLAGS=-buildvcs=false \
    "$GOIMG" sh -ec "apk add --no-cache gcc musl-dev patch >/dev/null && \
      mkdir /build && (cd /src && tar cf - --exclude=.git --exclude=./build .) | (cd /build && tar xf -) && \
      cd /build && patch -p1 </src/test/devnet/oldnode/embedded-refund-removal.patch && \
      go build -o /out/znnd -ldflags '-X $PKG.Version=$VERSION -X $PKG.GitCommit=$COMMIT' ./cmd/znnd && go version"
else
  docker run --rm \
    -v "$ROOT":/src:ro -v "$OUT":/out \
    -v go-zenon-test-gomod:/go/pkg/mod -v go-zenon-test-gocache:/root/.cache/go-build \
    -w /src -e CGO_ENABLED=1 -e GOWORK=off -e GOFLAGS=-buildvcs=false \
    "$GOIMG" sh -ec "apk add --no-cache gcc musl-dev >/dev/null && go build $OVERLAY -o /out/znnd -ldflags '-X $PKG.Version=$VERSION -X $PKG.GitCommit=$COMMIT' ./cmd/znnd && go version"
fi

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
