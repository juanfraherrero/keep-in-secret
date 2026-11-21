# Runs the full kis test suite against the git that a given distro ships, or
# against an exact git version built from source (GIT_VERSION).
#   docker build --build-arg BASE=ubuntu:22.04 -f test/docker/Dockerfile .
#   docker build --build-arg GIT_VERSION=2.30.2 -f test/docker/Dockerfile .
ARG BASE=debian:12
ARG GO_IMAGE=golang:1.27
FROM ${GO_IMAGE} AS go

FROM ${BASE}
ARG GIT_VERSION=
RUN if [ -n "$GIT_VERSION" ]; then \
      apt-get update && apt-get install -y --no-install-recommends \
        build-essential curl ca-certificates gettext libcurl4-openssl-dev libexpat1-dev libssl-dev zlib1g-dev openssh-client make && \
      curl -fsSL "https://mirrors.edge.kernel.org/pub/software/scm/git/git-$GIT_VERSION.tar.gz" | tar xz -C /tmp && \
      make -C "/tmp/git-$GIT_VERSION" -j"$(nproc)" prefix=/usr/local NO_TCLTK=1 NO_PERL=1 NO_PYTHON=1 all install >/dev/null && \
      rm -rf "/tmp/git-$GIT_VERSION" /var/lib/apt/lists/*; \
    elif command -v apt-get >/dev/null; then \
      apt-get update && apt-get install -y --no-install-recommends git openssh-client ca-certificates make && rm -rf /var/lib/apt/lists/*; \
    elif command -v apk >/dev/null; then \
      apk add --no-cache git openssh-keygen ca-certificates make; \
    elif command -v dnf >/dev/null; then \
      dnf install -y git-core openssh-clients make && dnf clean all; \
    fi
COPY --from=go /usr/local/go /usr/local/go
# Tests run as an unprivileged user, like a developer would.
RUN useradd -m dev 2>/dev/null || adduser -D dev
USER dev
ENV PATH=/usr/local/go/bin:$PATH CGO_ENABLED=0
WORKDIR /home/dev/src
COPY --chown=dev go.mod go.sum ./
RUN go mod download
COPY --chown=dev . .
RUN go test -c -o /dev/null ./cmd/kis
CMD git --version && go test -count=1 ./... && go test -count=1 -run TestScripts -v ./cmd/kis | grep -c -- '--- PASS: TestScripts/' | sed 's/$/ scenarios passed/'
