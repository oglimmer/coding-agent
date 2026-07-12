# coding-agent worker: clones a target repo, runs aider (DeepSeek) on a feature
# request, opens a PR, drives the GitHub Action review->fix loop, and merges.
#
# Ships Go + Node + JDK toolchains (Python comes with the base image) so aider's
# --auto-test loop can actually run the target repo's test suite: local
# compiler/test feedback is the main quality lever for the coding model.
FROM python:3.12-slim

ARG TARGETARCH
ARG GO_VERSION=1.26.4
ARG JAVA_VERSION=21

# git: clone/push; curl+jq: GitHub REST API and log-marker JSON.
# nodejs (NodeSource LTS): run JS/TS repo test suites (npm ci && npm test).
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    jq \
    ca-certificates \
    && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# Go toolchain: run Go repo test suites (go vet/test).
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
    | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:${PATH}"

# JDK: run JVM repo test suites via the Maven/Gradle wrapper (./mvnw, ./gradlew).
# Target repos may pin Java 21 (Spring Boot 4.x), newer than Debian bookworm's
# default-jdk (17), so fetch a pinned Temurin build instead — same "bake the
# toolchain, never download at runtime" rule as the Go install above. The
# Adoptium API redirects to the latest GA binary for the arch; map Docker's
# TARGETARCH (amd64/arm64) to Temurin's arch names (x64/aarch64).
RUN case "$TARGETARCH" in \
      amd64) JARCH=x64 ;; \
      arm64) JARCH=aarch64 ;; \
      *) echo "unsupported TARGETARCH: $TARGETARCH" >&2; exit 1 ;; \
    esac && \
    curl -fsSL "https://api.adoptium.net/v3/binary/latest/${JAVA_VERSION}/ga/linux/${JARCH}/jdk/hotspot/normal/eclipse" \
    | tar -C /opt -xz && \
    mv /opt/jdk-* /opt/java
ENV JAVA_HOME=/opt/java
ENV PATH="/opt/java/bin:${PATH}"

# Aider lives in its own venv so it never conflicts with a cloned repo's deps.
RUN python -m venv /opt/aider && \
    /opt/aider/bin/pip install --no-cache-dir aider-chat && \
    ln -s /opt/aider/bin/aider /usr/local/bin/aider

# pre-commit (own venv) so the pre-PR verify gate can run a target repo's
# .pre-commit-config.yaml hooks locally before opening a PR.
RUN python -m venv /opt/precommit && \
    /opt/precommit/bin/pip install --no-cache-dir pre-commit && \
    ln -s /opt/precommit/bin/pre-commit /usr/local/bin/pre-commit

RUN useradd --uid 1000 --create-home worker && \
    mkdir -p /work && chown worker:worker /work

COPY run_agent.sh /usr/local/bin/run_agent.sh
RUN chmod +x /usr/local/bin/run_agent.sh

USER worker
WORKDIR /work
# HOME=/work keeps Go's GOPATH/GOCACHE and npm's cache on the writable volume.
ENV HOME=/work
ENV GOPATH=/work/go
ENV GOCACHE=/work/.cache/go-build
# Use only the toolchain baked into this image. Never fetch a go toolchain at
# runtime: a cold `go test` in a fresh /work would otherwise spend most of the
# bounded baseline window downloading it, silently killing the auto-test loop.
# If a repo pins a newer Go than this image, fail fast (bump GO_VERSION instead).
ENV GOTOOLCHAIN=local

# Build provenance, baked in last so it never busts the toolchain layer cache.
# oglimmer.sh already passes these --build-arg values to every image.
ARG GIT_COMMIT=none
ARG VERSION=dev
ARG BUILD_TIME=unknown
ENV WORKER_GIT_COMMIT=$GIT_COMMIT \
    WORKER_VERSION=$VERSION \
    WORKER_BUILD_TIME=$BUILD_TIME

ENTRYPOINT ["/usr/local/bin/run_agent.sh"]
