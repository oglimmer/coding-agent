#!/usr/bin/env bash
# Build / test / run helper for the coding-agent project.
#
# Usage:
#   ./oglimmer.sh test                 run backend + frontend test suites
#   ./oglimmer.sh build [-b|-f|-w|-a]  build docker images (backend/frontend/worker/all)
#   ./oglimmer.sh start | stop | logs  local compose stack (postgres + backend)
#   ./oglimmer.sh dev                  hints for running the dev servers
#
# Flags for build:
#   --platform auto|arm64|amd64|multi   (default: auto)
#   --no-push                           build only, do not push
#   --registries r1,r2                  push targets (default: ghcr.io/oglimmer)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="$(node -p "require('${ROOT}/frontend/package.json').version" 2>/dev/null || echo "0.0.0")"
GIT_COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo none)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
REGISTRY="ghcr.io/oglimmer"
PLATFORM="auto"
PUSH=true

# Restart hook — triggers an in-cluster rollout after new :latest images are
# pushed. CI build runners have no kubectl/cluster access, so oglimmer.sh POSTs
# to the hook (authenticated with RESTART_TOKEN) instead. Disabled automatically
# when RESTART_TOKEN is unset (e.g. local builds) or when --no-push is given.
RESTART_HOOK_URL="${RESTART_HOOK_URL:-https://restart.oglimmer.com/restart}"
K8S_NAMESPACE="${K8S_NAMESPACE:-coding-agent}"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31mERROR:\033[0m %s\n' "$*" >&2; exit 1; }

cmd_test() {
  log "backend: gofmt / vet / test"
  ( cd "$ROOT/backend" && gofmt -l . | (grep . && die "gofmt: files need formatting" || true) \
      && go vet ./... && go test -race ./... )
  log "frontend: typecheck / lint / test"
  ( cd "$ROOT/frontend" && npm run check )
  log "worker: shellcheck"
  shellcheck "$ROOT/worker/run_agent.sh"
  log "all checks passed"
}

resolve_platform_arg() {
  case "$PLATFORM" in
    auto)  echo "" ;;
    arm64) echo "--platform linux/arm64" ;;
    amd64) echo "--platform linux/amd64" ;;
    multi) echo "--platform linux/amd64,linux/arm64" ;;
    *)     die "unknown platform: $PLATFORM" ;;
  esac
}

# Trigger an in-cluster rollout of a deployment via the restart hook. Called
# after a successful push so the freshly pushed :latest image is picked up.
# No-op (skipped) unless RESTART_TOKEN is set; worker has no deployment.
restart_via_hook() {
  local deployment="$1"
  if [ -z "${RESTART_TOKEN:-}" ]; then
    log "RESTART_TOKEN not set — skipping rollout of ${deployment}"
    return 0
  fi
  local url="${RESTART_HOOK_URL}/${K8S_NAMESPACE}/${deployment}"
  log "restarting ${deployment} via hook: ${url}"
  if ! curl -fsS -X POST -H "Authorization: Bearer ${RESTART_TOKEN}" "$url" >/dev/null; then
    die "failed to trigger restart for ${deployment} via hook"
  fi
}

build_image() {
  local name="$1" context="$2"; shift 2
  local tag="${REGISTRY}/coding-agent-${name}:${VERSION}"
  local latest="${REGISTRY}/coding-agent-${name}:latest"
  local plat; plat="$(resolve_platform_arg)"
  log "building ${tag}"
  # shellcheck disable=SC2086
  docker build $plat \
    --build-arg VERSION="$VERSION" \
    --build-arg GIT_COMMIT="$GIT_COMMIT" \
    --build-arg BUILD_TIME="$BUILD_TIME" \
    --build-arg VITE_APP_VERSION="$VERSION" \
    --build-arg VITE_GIT_COMMIT="$GIT_COMMIT" \
    --build-arg VITE_BUILD_TIME="$BUILD_TIME" \
    -t "$tag" -t "$latest" "$context" "$@"
  if [ "$PUSH" = true ]; then
    log "pushing ${tag}"
    docker push "$tag"
    docker push "$latest"
    # backend/frontend run as Deployments and need a rollout to pick up the new
    # :latest image; worker runs as ad-hoc Jobs, so there is nothing to restart.
    case "$name" in
      backend|frontend) restart_via_hook "coding-agent-${name}" ;;
    esac
  fi
}

cmd_build() {
  local what="all"
  local args=()
  while [ $# -gt 0 ]; do
    case "$1" in
      -b) what="backend" ;;
      -f) what="frontend" ;;
      -w) what="worker" ;;
      -a) what="all" ;;
      --platform) PLATFORM="$2"; shift ;;
      --no-push) PUSH=false ;;
      --registries) REGISTRY="$2"; shift ;;
      *) args+=("$1") ;;
    esac
    shift
  done
  case "$what" in
    backend)  build_image backend  "$ROOT/backend"  ${args[@]+"${args[@]}"} ;;
    frontend) build_image frontend "$ROOT/frontend" ${args[@]+"${args[@]}"} ;;
    worker)   build_image worker   "$ROOT/worker"   ${args[@]+"${args[@]}"} ;;
    all)
      build_image backend  "$ROOT/backend"
      build_image frontend "$ROOT/frontend"
      build_image worker   "$ROOT/worker"
      ;;
  esac
}

cmd_start() { ( cd "$ROOT" && docker compose up -d --build ); }
cmd_stop()  { ( cd "$ROOT" && docker compose down ); }
cmd_logs()  { ( cd "$ROOT" && docker compose logs -f ); }

cmd_dev() {
  cat <<EOF
Local development:
  1) ./oglimmer.sh start            # Postgres + backend on :8080
  2) cd frontend && npm run dev     # Vite dev server on :5173 (proxies /api)
  Sign in with the dev password (AUTH_MODE=password, DEV_PASSWORD=dev).
  The first user to sign in becomes admin.
EOF
}

case "${1:-}" in
  test)  cmd_test ;;
  build) shift; cmd_build "$@" ;;
  start) cmd_start ;;
  stop)  cmd_stop ;;
  logs)  cmd_logs ;;
  dev)   cmd_dev ;;
  *) die "usage: $0 {test|build [-b|-f|-w|-a]|start|stop|logs|dev}" ;;
esac
