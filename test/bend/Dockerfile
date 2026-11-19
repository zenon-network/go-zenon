# The Bend 2 checker in a container, for test/bend/run.sh (docs/BEND_LAWS.md).
# The release archive is the one bend-lang.com/install.sh installs; the
# checksums below are the ones that script carries for this version. To move to
# a newer Bend, change BEND_VERSION and both checksums together.
# The base image is named by digest (debian:bookworm-slim on 2026-11-19), so
# the same bytes are used until someone changes this line.
FROM debian@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc
ARG BEND_VERSION=2.0.40
ARG BEND_SHA_ARM64=2f42a6fa34ecdaa71ae105e5fa734f9a07e129d333e45448ab6d2ff03a837570
ARG BEND_SHA_X64=c9ff8d67bd903fb6ad9a26b43d8a62c01021de82a44d55ce4a3dcd4732113d6b
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*
RUN set -eu; \
    case "$(uname -m)" in \
      aarch64|arm64) arch=arm64; sum=$BEND_SHA_ARM64;; \
      x86_64|amd64)  arch=x64;   sum=$BEND_SHA_X64;; \
      *) echo "unsupported CPU: $(uname -m)" >&2; exit 1;; \
    esac; \
    name="bend-$BEND_VERSION-linux-$arch.tar.gz"; \
    curl --proto '=https' --tlsv1.2 -fsSL -o "/tmp/$name" \
      "https://github.com/bendlang/bend/releases/download/v$BEND_VERSION/$name"; \
    echo "$sum  /tmp/$name" | sha256sum -c -; \
    mkdir -p /opt && tar -xzf "/tmp/$name" -C /opt && rm "/tmp/$name"
# bend asks bend-lang.com for the latest version once a day unless this is set.
# run.sh also starts the container without a network.
ENV BEND_NO_TELEMETRY=1 PATH=/opt/bend/bin:$PATH HOME=/tmp
RUN useradd -m -u 10001 bend
USER bend
WORKDIR /work
ENTRYPOINT ["bend"]
