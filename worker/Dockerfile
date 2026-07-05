# coding-agent worker: clones a target repo, runs aider (DeepSeek) on a feature
# request, opens a PR, drives the GitHub Action review->fix loop, and merges.
#
# Ships Go + Node toolchains (Python comes with the base image) so aider's
# --auto-test loop can actually run the target repo's test suite: local
# compiler/test feedback is the main quality lever for the coding model.
FROM python:3.12-slim

ARG TARGETARCH
ARG GO_VERSION=1.26.0

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

# Aider lives in its own venv so it never conflicts with a cloned repo's deps.
RUN python -m venv /opt/aider && \
    /opt/aider/bin/pip install --no-cache-dir aider-chat && \
    ln -s /opt/aider/bin/aider /usr/local/bin/aider

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

ENTRYPOINT ["/usr/local/bin/run_agent.sh"]
