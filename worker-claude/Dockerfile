# coding-agent worker (Claude Code / DeepSeek variant): clones a target repo,
# runs Claude Code (pointed at a DeepSeek backend) on a feature request, opens a
# PR, drives the GitHub Action review->fix loop, and merges. The twin of the
# aider worker image; only the coding engine differs.
#
# Ships Go + Node toolchains (Python comes with the base image) so the pre-PR
# verify gate can run the target repo's build/lint/test command, and so Claude
# Code (a Node CLI) can run the repo's test suite itself while it works.
FROM python:3.12-slim

ARG TARGETARCH
ARG GO_VERSION=1.26.4
ARG CLAUDE_CODE_VERSION=latest

# git: clone/push; curl+jq: GitHub REST API and log-marker JSON.
# nodejs (NodeSource LTS): Claude Code runtime + JS/TS repo test suites.
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    jq \
    ca-certificates \
    && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# Go toolchain: run Go repo build/test suites (go build/vet/test).
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
    | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:${PATH}"

# Claude Code CLI (the coding engine). Installed globally; pointed at DeepSeek at
# runtime purely via ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN (see run_agent.sh).
RUN npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" \
    && npm cache clean --force

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
# HOME=/work keeps Go's GOPATH/GOCACHE, npm's cache and Claude Code's config on
# the writable volume.
ENV HOME=/work
ENV GOPATH=/work/go
ENV GOCACHE=/work/.cache/go-build
# Use only the toolchain baked into this image. Never fetch a go toolchain at
# runtime: a cold `go test` in a fresh /work would otherwise spend most of the
# bounded baseline window downloading it, silently killing the auto-test loop.
# If a repo pins a newer Go than this image, fail fast (bump GO_VERSION instead).
ENV GOTOOLCHAIN=local

# Build provenance, baked in last so it never busts the toolchain layer cache.
ARG GIT_COMMIT=none
ARG VERSION=dev
ARG BUILD_TIME=unknown
ENV WORKER_GIT_COMMIT=$GIT_COMMIT \
    WORKER_VERSION=$VERSION \
    WORKER_BUILD_TIME=$BUILD_TIME

ENTRYPOINT ["/usr/local/bin/run_agent.sh"]
