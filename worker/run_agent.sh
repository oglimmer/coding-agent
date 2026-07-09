#!/usr/bin/env bash
# coding-agent worker pipeline:
#   clone repo -> scope (model picks relevant files + test command) -> aider in
#   architect mode implements the feature (with tests, auto-running the repo's
#   test suite after each edit) -> pre-PR self-review gate -> push -> open PR ->
#   wait for the repo's GitHub Action review -> fix any findings with aider ->
#   re-review -> squash-merge -> emit result line.
#
# Required env:
#   GITHUB_TOKEN        fine-grained PAT / app token (contents:write, pull_requests:write)
#   AGENT_REPO          owner/name
#   AGENT_BRANCH        branch to create
#   AGENT_PROMPT        full instruction for the coding agent
#   AGENT_PR_TITLE      PR title
# Optional env:
#   AGENT_BASE_BRANCH   default: main
#   AGENT_FEATURE       raw feature text (for the PR body / self-review)
#   AGENT_VERIFY_CMD    repo's build/lint/test command; run as a hard gate before
#                       the PR (falls back to the detected inner-loop command)
#   AGENT_TEST_CMD      repo's fast inner-loop test command for aider --auto-test;
#                       empty = detect it from the repo's manifests
#   VERIFY_MAX_ROUNDS   corrective aider rounds for the verify gate (default: 2)
#   AIDER_MODEL         architect (planning) model, default: deepseek/deepseek-v4-pro
#   AIDER_EDITOR_MODEL  editing model, default: deepseek/deepseek-chat
#   AIDER_MAP_TOKENS    repo-map budget, default: 4096
#   DEEPSEEK_API_KEY    consumed by aider and the helper calls below
#   GITHUB_BOT_LOGIN    reviewer login to treat as "self" (ignored when waiting)
#   REVIEW_MAX_ROUNDS   default: 3
#   REVIEW_TIMEOUT      seconds to wait per review round (default: 1800)
#   REVIEW_POLL         seconds between polls (default: 20)
#   NO_CHECK_GRACE      polls before trusting a "no checks" reading (default: 3)
#   DEEPSEEK_BASE_URL   helper-call API base (default: https://api.deepseek.com)
#   SCOPE_MODEL         model for task scoping (file selection) (default: deepseek-chat)
#   REVIEW_JUDGE_MODEL  model for self-review + review-judging (default: deepseek-chat)
#   SELF_REVIEW_ROUNDS  pre-PR corrective rounds, default: 2
#   AIDER_TIMEOUT       seconds per aider round before it is killed (default: 3600)
#   AIDER_TEMPERATURE   sampling temperature (default: 0.2 — DeepSeek loops at 0)
#   AIDER_FREQUENCY_PENALTY  anti-repetition penalty (default: 0.3)
set -uo pipefail

BASE_BRANCH="${AGENT_BASE_BRANCH:-main}"
# Architect mode: AIDER_MODEL reasons about WHAT to change, AIDER_EDITOR_MODEL
# turns the plan into precise edits. Splitting the two is aider's biggest
# documented quality lever for mid-tier models.
AIDER_MODEL="${AIDER_MODEL:-deepseek/deepseek-v4-pro}"
AIDER_EDITOR_MODEL="${AIDER_EDITOR_MODEL:-deepseek/deepseek-chat}"
AIDER_MAP_TOKENS="${AIDER_MAP_TOKENS:-4096}"
BOT_LOGIN="${GITHUB_BOT_LOGIN:-coding-agent-bot}"
REVIEW_MAX_ROUNDS="${REVIEW_MAX_ROUNDS:-3}"
REVIEW_TIMEOUT="${REVIEW_TIMEOUT:-1800}"
REVIEW_POLL="${REVIEW_POLL:-20}"
# Polls to wait before trusting a "0 check runs" reading (repo has no checks vs.
# a just-pushed commit whose checks have not registered yet).
NO_CHECK_GRACE="${NO_CHECK_GRACE:-3}"
# Many reviewers (e.g. GitHub Action AI reviewers) never emit a formal
# APPROVED/CHANGES_REQUESTED verdict — their real judgment is prose in a COMMENTED
# review. We classify that prose with a model before merging. Uses the same
# DeepSeek credentials as the coding model.
DEEPSEEK_BASE_URL="${DEEPSEEK_BASE_URL:-https://api.deepseek.com}"
REVIEW_JUDGE_MODEL="${REVIEW_JUDGE_MODEL:-deepseek-chat}"
# Scoping is file-path retrieval over a flat listing, not reasoning — the cheap
# chat tier is enough, and scoping fails OPEN so a miss just costs a curated file
# set. Split from REVIEW_JUDGE_MODEL so the diff-quality judge (self-review +
# review-judge, a genuine reasoning task) can be tuned up independently.
SCOPE_MODEL="${SCOPE_MODEL:-deepseek-chat}"
SELF_REVIEW_ROUNDS="${SELF_REVIEW_ROUNDS:-2}"
# Hard per-round bound: a model stuck in a repetition loop must not ride out the
# whole Job deadline.
AIDER_TIMEOUT="${AIDER_TIMEOUT:-3600}"
# DeepSeek degenerates into verbatim repetition loops at temperature 0 (aider's
# default) on long generations; a little temperature + frequency penalty is the
# standard countermeasure.
AIDER_TEMPERATURE="${AIDER_TEMPERATURE:-0.2}"
AIDER_FREQUENCY_PENALTY="${AIDER_FREQUENCY_PENALTY:-0.3}"
# Authoritative pre-PR verification: the repo's real build/lint/test command (the
# same one CI runs). Empty falls back to the detected SCOPE_TEST_CMD so the final
# diff is still checked once before the PR even for unconfigured repos.
VERIFY_CMD="${AGENT_VERIFY_CMD:-}"
# Optional repo-configured inner-loop test command (aider --auto-test). Empty =
# detect it from the repo's manifests. Lets an owner override detection for a
# non-standard setup without making their heavy VERIFY_CMD run after every edit.
TEST_CMD="${AGENT_TEST_CMD:-}"
VERIFY_MAX_ROUNDS="${VERIFY_MAX_ROUNDS:-2}"
REPO_DIR=/work/repo
API="https://api.github.com/repos/${AGENT_REPO:-}"

emit_result() {
  # single-line JSON so the backend can grep it out of the pod logs
  echo "CODING_AGENT_RESULT:$1"
}

fail() {
  emit_result "$(jq -cn --arg r "$1" --arg b "${AGENT_BRANCH:-}" \
    '{status:"failed", reason:$r, branch:$b}')"
  exit 1
}

gh_api() {
  # gh_api METHOD URL [DATA] — echoes the response body. Returns non-zero on a
  # network error or an HTTP status >= 400, so callers can tell a real failure
  # from an empty-but-valid result (a rate-limited poll must NOT read as "no
  # checks"). Idempotent GETs are retried a couple of times on 429/5xx/network.
  local method="$1" url="$2" data="${3:-}"
  local attempt=0 resp curl_rc code body
  while :; do
    attempt=$((attempt + 1))
    if [ -n "$data" ]; then
      resp=$(curl -sS --max-time 60 -w $'\n%{http_code}' -X "$method" \
        -H "Authorization: Bearer ${GITHUB_TOKEN}" \
        -H "Accept: application/vnd.github+json" \
        "$url" -d "$data")
    else
      resp=$(curl -sS --max-time 60 -w $'\n%{http_code}' -X "$method" \
        -H "Authorization: Bearer ${GITHUB_TOKEN}" \
        -H "Accept: application/vnd.github+json" \
        "$url")
    fi
    curl_rc=$?
    # GitHub REST bodies are single-line JSON; %{http_code} follows our literal
    # newline, so split on the last newline regardless of body content.
    code="${resp##*$'\n'}"
    body="${resp%$'\n'*}"
    if [ "$curl_rc" -ne 0 ]; then
      [ "$method" = GET ] && [ "$attempt" -lt 3 ] && { sleep 5; continue; }
      echo "WARN gh_api ${method} network error (curl rc=${curl_rc})" >&2
      printf '%s' "$body"; return 1
    fi
    if [ "${code:-0}" -ge 400 ] 2>/dev/null; then
      if [ "$method" = GET ] && [ "$attempt" -lt 3 ] \
         && { [ "$code" = 429 ] || [ "$code" -ge 500 ]; }; then
        sleep 5; continue
      fi
      echo "WARN gh_api ${method} -> HTTP ${code}" >&2
      printf '%s' "$body"; return 1
    fi
    printf '%s' "$body"; return 0
  done
}

# deepseek_call SYSTEM USER [MODEL] — one-shot chat completion against a helper
# model; echoes the assistant content, empty output on any failure (callers fail
# open or fail safe as appropriate to their gate). MODEL defaults to
# REVIEW_JUDGE_MODEL (the judging tier); scoping passes SCOPE_MODEL to override it.
deepseek_call() {
  local sys="$1" user="$2" model="${3:-$REVIEW_JUDGE_MODEL}" payload resp
  payload=$(jq -cn --arg m "$model" --arg s "$sys" --arg u "$user" \
    '{model:$m, temperature:0, messages:[{role:"system",content:$s},{role:"user",content:$u}]}') || return 1
  resp=$(curl -sS --max-time 120 -X POST "${DEEPSEEK_BASE_URL}/chat/completions" \
    -H "Authorization: Bearer ${DEEPSEEK_API_KEY:-}" \
    -H "Content-Type: application/json" \
    -d "$payload") || return 1
  echo "$resp" | jq -r '.choices[0].message.content // ""'
}

# Pull the first {...} JSON object out of model prose (tolerates ```json fences).
extract_json() {
  tr '\n' ' ' | grep -o '{.*}' | head -1
}

for var in GITHUB_TOKEN AGENT_REPO AGENT_BRANCH AGENT_PROMPT AGENT_PR_TITLE; do
  [ -n "${!var:-}" ] || fail "missing required env var: $var"
done

# Provenance + effective config, printed into the log so a run can be analysed
# later (this block is persisted with the log even after the pod is gone). The
# worker_commit is baked into the image at build time.
echo "=== job metadata ==="
echo "worker_commit:  ${WORKER_GIT_COMMIT:-none}"
echo "worker_version: ${WORKER_VERSION:-dev}"
echo "worker_built:   ${WORKER_BUILD_TIME:-unknown}"
echo "repo:           ${AGENT_REPO}  base=${BASE_BRANCH}"
echo "model:          ${AIDER_MODEL}  editor=${AIDER_EDITOR_MODEL}"
echo "helper_models:  scope=${SCOPE_MODEL}  judge=${REVIEW_JUDGE_MODEL}"
echo "review_rounds:  ${REVIEW_MAX_ROUNDS}  verify_rounds=${VERIFY_MAX_ROUNDS}  aider_timeout=${AIDER_TIMEOUT}s"
echo "verify_cmd:     ${VERIFY_CMD:-<detected>}"
echo "test_cmd:       ${TEST_CMD:-<detected>}"
echo "deepseek_base:  ${DEEPSEEK_BASE_URL}"
echo "===================="

echo "=== coding-agent worker: cloning ${AGENT_REPO} ==="
git clone "https://x-access-token:${GITHUB_TOKEN}@github.com/${AGENT_REPO}.git" \
  "$REPO_DIR" || fail "git clone failed"
cd "$REPO_DIR" || fail "repo dir missing"

git config user.name "$BOT_LOGIN"
git config user.email "${BOT_LOGIN}@users.noreply.github.com"
git checkout -b "$AGENT_BRANCH" "origin/${BASE_BRANCH}" || fail "branch creation failed"

# --- scoping -------------------------------------------------------------------
# aider only edits well what it can see. Ask the model — from the full file
# listing — which files matter for this feature, what command validates the
# change, and where the change belongs. Everything here fails OPEN (empty
# results = today's behaviour), never blocks the run.
SCOPE_FILES=()
SCOPE_TEST_CMD=""
SCOPE_NOTES=""

# --- test-command detection ----------------------------------------------------
# The inner-loop test command (aider --auto-test) is DETECTED from the repo's real
# manifests, never invented by a model: a hallucinated script like `test:unit`
# (job 226cfe38) is impossible when every command is built from files that exist.
# Precedence: explicit repo config (AGENT_TEST_CMD) > detection from the modules the
# change touches (SCOPE_FILES) > detection from the repo's top-level modules.

# True if DIR holds a recognised build manifest.
module_manifest() {
  local d="$1"
  [ -f "$d/go.mod" ] || [ -f "$d/package.json" ] || [ -f "$d/pyproject.toml" ] \
    || [ -f "$d/requirements.txt" ] || [ -f "$d/Makefile" ]
}

# Nearest ancestor of FILE (its own dir upward) that holds a manifest. Echoes the
# repo-root-relative dir ("" = repo root); returns non-zero if none up to the root.
nearest_module_dir() {
  local d; d=$(dirname "$1")
  while :; do
    if [ "$d" = "." ]; then
      module_manifest "." && { echo ""; return 0; }
      return 1
    fi
    module_manifest "$d" && { echo "$d"; return 0; }
    d=$(dirname "$d")
  done
}

# The fastest per-edit validation command for the module in DIR (run from DIR),
# derived from the manifest that is actually present. Empty if the module exposes
# no usable signal. npm scripts are read from package.json so a script name can
# never be hallucinated; the npm-init placeholder `test` script is skipped.
module_cmd() {
  local d="$1" s t
  if [ -f "$d/go.mod" ]; then
    echo "go test ./..."
  elif [ -f "$d/package.json" ]; then
    for s in test typecheck build; do
      jq -e --arg s "$s" '.scripts[$s]' "$d/package.json" >/dev/null 2>&1 || continue
      if [ "$s" = test ]; then
        t=$(jq -r '.scripts.test' "$d/package.json")
        case "$t" in *"no test specified"*|*"exit 1"*) continue ;; esac
      fi
      echo "npm run $s"; return 0
    done
  elif [ -f "$d/pyproject.toml" ] || [ -f "$d/requirements.txt" ]; then
    echo "pytest"
  elif [ -f "$d/Makefile" ]; then
    grep -qE '^test[[:space:]]*:' "$d/Makefile" && echo "make test"
  fi
}

# Relative path FROM one repo-root-relative dir TO another ("" = repo root), so a
# multi-module command can `cd ../sibling` between stacks the way prepare_cmd_deps
# expects (it accumulates cd segments and lets the filesystem collapse the `..`).
relpath() {
  local from="$1" to="$2" i=0 j rel=""
  [ "$from" = "$to" ] && { echo ""; return; }
  local -a fa=() ta=()
  [ -n "$from" ] && IFS=/ read -ra fa <<<"$from"
  [ -n "$to" ]   && IFS=/ read -ra ta <<<"$to"
  while [ $i -lt ${#fa[@]} ] && [ $i -lt ${#ta[@]} ] && [ "${fa[$i]}" = "${ta[$i]}" ]; do
    i=$((i + 1))
  done
  for ((j = i; j < ${#fa[@]}; j++)); do rel="../$rel"; done
  for ((j = i; j < ${#ta[@]}; j++)); do rel="$rel${ta[$j]}/"; done
  echo "${rel%/}"
}

# Populate SCOPE_TEST_CMD deterministically. Fails open (empty command) exactly as
# a failed model guess used to, so the fallback compile/vet gate still applies.
detect_test_cmd() {
  if [ -n "$TEST_CMD" ]; then
    SCOPE_TEST_CMD="$TEST_CMD"
    echo "=== test command from repo config: ${SCOPE_TEST_CMD} ==="
    return 0
  fi
  local -a mods=()
  local f d m
  # Modules the change is expected to touch, inferred from the scoped files.
  for f in ${SCOPE_FILES[@]+"${SCOPE_FILES[@]}"}; do
    d=$(nearest_module_dir "$f") || continue
    case " ${mods[*]-} " in *" $d "*) ;; *) mods+=("$d") ;; esac
  done
  # No scoped files (scoping failed / matched nothing): fall back to the repo's
  # top-level modules so aider still gets a signal.
  if [ ${#mods[@]} -eq 0 ]; then
    while IFS= read -r m; do
      d=$(dirname "$m"); [ "$d" = "." ] && d=""
      case " ${mods[*]-} " in *" $d "*) ;; *) mods+=("$d") ;; esac
    done < <(git ls-files | grep -E '(^|/)(go\.mod|package\.json|pyproject\.toml)$' \
              | grep -v node_modules | head -20)
  fi
  # Chain one `cd`-relative segment per module that yields a command.
  local cur="" cmd="" sub cd part
  for d in ${mods[@]+"${mods[@]}"}; do
    sub=$(module_cmd "$d"); [ -n "$sub" ] || continue
    cd=$(relpath "$cur" "$d")
    part=""; [ -n "$cd" ] && part="cd $cd && "
    part="$part$sub"
    [ -n "$cmd" ] && cmd="$cmd && $part" || cmd="$part"
    cur="$d"
  done
  SCOPE_TEST_CMD="$cmd"
}

scope_repo() {
  local listing content json sys user
  listing=$(git ls-files | head -3000)
  sys='You prepare a coding task for an automated coding agent. Given a feature request and the repository file listing, reply with ONLY a compact JSON object: {"files":["path",...],"notes":"..."}. "files": 5-15 EXISTING paths from the listing that are most relevant — the files that must change, plus key context (routing, templates, similar features, the tests to mirror). "notes": 2-4 sentences on WHERE the feature belongs and how to implement it idiomatically in this codebase.'
  user=$(printf 'FEATURE REQUEST:\n%s\n\nREPOSITORY FILE LISTING:\n%s\n' \
    "${AGENT_FEATURE:-$AGENT_PROMPT}" "$listing")
  content=$(deepseek_call "$sys" "$user" "$SCOPE_MODEL") || return 0
  json=$(printf '%s' "$content" | extract_json) || return 0
  [ -n "$json" ] || return 0

  local f
  while IFS= read -r f; do
    [ -f "$f" ] && SCOPE_FILES+=("$f")
  done < <(echo "$json" | jq -r '.files[]? // empty' 2>/dev/null | head -15)
  SCOPE_NOTES=$(echo "$json" | jq -r '.notes // ""' 2>/dev/null)
}

echo "=== scoping the task (${SCOPE_MODEL}) ==="
scope_repo
echo "=== scope: ${#SCOPE_FILES[@]} files ==="
[ -n "$SCOPE_NOTES" ] && echo "=== scope notes: ${SCOPE_NOTES} ==="
# Derive the inner-loop test command from real manifests (runs regardless of
# whether scoping succeeded, so a failed scope still yields a signal).
detect_test_cmd
echo "=== detected test_cmd='${SCOPE_TEST_CMD:-none}' ==="

# The test command is only useful if it can actually run: install the deps every
# stack it touches, then require it to PASS on the untouched branch. A command
# that fails at baseline (missing deps, broken env, a hallucinated script name)
# would send aider chasing phantom failures instead of the feature — exactly the
# confusion spiral we must avoid.

disable_auto_test() {
  echo "=== ${1}; disabling auto-test ==="
  SCOPE_TEST_CMD=""
}

# Install node deps in DIR when it has a package.json and no node_modules yet.
prepare_npm_dir() {
  local dir="$1"
  [ -f "$dir/package.json" ] || return 0
  [ -d "$dir/node_modules" ] && return 0
  echo "=== installing npm deps in ${dir} ==="
  ( cd "$dir" && { npm ci --no-audit --no-fund || npm install --no-audit --no-fund; } ) \
    >/work/npm-install.log 2>&1 || { tail -5 /work/npm-install.log; return 1; }
}

# For an "npm run <script>" segment, confirm <script> is defined in DIR's
# package.json. A hallucinated script name (job 226cfe38's `test:unit`) otherwise
# only surfaces as a baseline failure that silently kills the whole auto-test loop.
check_npm_script() {
  local dir="$1" seg="$2" script
  script=$(printf '%s' "$seg" | sed -n 's/.*npm run \([^ ]*\).*/\1/p')
  [ -n "$script" ] || return 0
  [ -f "$dir/package.json" ] || return 0
  jq -e --arg s "$script" '.scripts[$s]' "$dir/package.json" >/dev/null 2>&1 && return 0
  echo "=== npm script '${script}' is not defined in ${dir}/package.json ==="
  return 1
}

# Create a single shared target venv (once) and install DIR's Python deps into it,
# exporting it onto PATH so both the baseline run and aider's --test-cmd subprocess
# use it. Runs in the current shell (via `< <(...)` below), so the export persists.
TARGET_VENV=""
prepare_python() {
  local dir="$1"
  if [ -z "$TARGET_VENV" ]; then
    TARGET_VENV=/work/target-venv
    python -m venv "$TARGET_VENV" >/work/py-install.log 2>&1 || return 1
    export PATH="$TARGET_VENV/bin:$PATH"
    pip install -q pytest >>/work/py-install.log 2>&1 || true
  fi
  if [ -f "$dir/requirements.txt" ]; then
    pip install -q -r "$dir/requirements.txt" >>/work/py-install.log 2>&1 \
      || { tail -5 /work/py-install.log; return 1; }
  fi
  [ -f "$dir/pyproject.toml" ] && pip install -q -e "$dir" >>/work/py-install.log 2>&1
  return 0
}

# Walk CMD's &&-joined segments, tracking the working directory as `cd` segments
# change, and prepare each stack the command actually touches. A cross-stack
# command like "cd backend && go test ./... && cd ../frontend && npm run test:unit"
# therefore gets BOTH stacks prepared, not just the first cd. Returns non-zero if a
# required install failed or an npm script it names does not exist. Shared by the
# auto-test prep and the verify-command baseline check.
prepare_cmd_deps() {
  local cmd="$1" seg tok dir="."
  # `|| [ -n "$seg" ]` keeps the LAST segment: sed leaves no trailing newline, so a
  # plain `while read` would silently drop it — and the final segment of a
  # cross-stack command is usually the second stack's `npm`/`pytest` test (the very
  # thing whose deps must be installed and whose script must be checked).
  while IFS= read -r seg || [ -n "$seg" ]; do
    seg="${seg#"${seg%%[![:space:]]*}"}"      # ltrim
    tok="${seg%% *}"
    case "$tok" in
      cd)
        # Accumulate the path: `cd backend && cd ../frontend` must resolve to
        # `frontend`, not `../frontend`. Absolute targets replace; relative ones
        # append and let the filesystem collapse the `..`.
        local d="${seg#cd }"; d="${d%% *}"
        if [ -z "$d" ]; then :
        elif [ "${d#/}" != "$d" ]; then dir="$d"
        else dir="$dir/$d"
        fi ;;
      npm|npx|node)
        prepare_npm_dir "$dir" || return 1
        check_npm_script "$dir" "$seg" || return 1 ;;
      python|python3|pytest)
        prepare_python "$dir" || return 1 ;;
    esac
  done < <(printf '%s' "$cmd" | sed 's/&&/\n/g')
  return 0
}

prepare_test_cmd() {
  [ -n "$SCOPE_TEST_CMD" ] || return 0
  prepare_cmd_deps "$SCOPE_TEST_CMD" || { disable_auto_test "test-command setup failed"; return 0; }
  echo "=== verifying test command on clean baseline ==="
  if bash -c "$SCOPE_TEST_CMD" >/work/baseline-test.log 2>&1; then
    echo "=== baseline green: auto-test enabled ==="
  else
    echo "=== test command fails on baseline; disabling auto-test ==="
    tail -5 /work/baseline-test.log
    SCOPE_TEST_CMD=""
  fi
}
prepare_test_cmd

# The repo-configured verify command is the authoritative pre-PR gate, but if it is
# already RED on untouched main (broken config, missing tool, red base) aider can
# never make it green — it would burn the whole run and every verify round for
# nothing (this is still the clean baseline: aider has not run yet). Check it once
# and fail fast with an actionable reason instead.
if [ -n "$VERIFY_CMD" ]; then
  echo "=== checking repo verify command on clean baseline: ${VERIFY_CMD} ==="
  prepare_cmd_deps "$VERIFY_CMD" || true
  if bash -c "$VERIFY_CMD" >/work/verify-baseline.log 2>&1; then
    echo "=== verify command green at baseline ==="
  else
    echo "=== repo verify command is RED at baseline ==="
    tail -20 /work/verify-baseline.log
    fail "repo verify command (AGENT_VERIFY_CMD) fails on untouched ${BASE_BRANCH}; fix the repository's verify setting — not spending an agent run on it"
  fi
fi

# When no usable test command survived, fall back to a zero-config compile/type
# gate so aider still gets a syntax/type signal instead of coding blind — the
# missing quality lever behind job 226cfe38's hour-long, no-feedback timeout. The
# fallback is built from the repo's own stack (not model input) and must itself
# pass at baseline, else we drop it and leave auto-test off (today's behaviour).
derive_fallback_test_cmd() {
  [ -z "$SCOPE_TEST_CMD" ] || return 0
  local cand="" gomod pkg pdir
  gomod=$(git ls-files | grep -E '(^|/)go\.mod$' | head -1)
  if [ -n "$gomod" ] && command -v go >/dev/null 2>&1; then
    local gdir; gdir=$(dirname "$gomod")
    [ "$gdir" = "." ] && cand="go build ./... && go vet ./..." \
                      || cand="cd $gdir && go build ./... && go vet ./..."
  else
    pkg=$(git ls-files | grep -E '(^|/)package\.json$' | grep -v node_modules | head -1)
    if [ -n "$pkg" ] && jq -e '.scripts.build' "$pkg" >/dev/null 2>&1; then
      pdir=$(dirname "$pkg")
      [ "$pdir" = "." ] && cand="npm run build" || cand="cd $pdir && npm run build"
      prepare_npm_dir "$pdir" || cand=""
    fi
  fi
  [ -n "$cand" ] || return 0
  echo "=== no test command; trying zero-config fallback gate: ${cand} ==="
  if bash -c "$cand" >/work/fallback-test.log 2>&1; then
    echo "=== fallback gate green: using it as the aider test loop ==="
    SCOPE_TEST_CMD="$cand"
  else
    echo "=== fallback gate red at baseline; leaving auto-test disabled ==="
    tail -5 /work/fallback-test.log
  fi
}
derive_fallback_test_cmd

# --- aider ---------------------------------------------------------------------
# Per-model overrides: temperature + frequency penalty keep DeepSeek out of
# repetition loops; max_tokens bounds a single response. Entries fully specify
# the DeepSeek-appropriate settings since a file entry replaces the built-in one.
cat > /work/aider-model-settings.yml <<EOF
- name: ${AIDER_MODEL}
  edit_format: diff
  use_repo_map: true
  examples_as_sys_msg: true
  extra_params:
    max_tokens: 8192
    temperature: ${AIDER_TEMPERATURE}
    frequency_penalty: ${AIDER_FREQUENCY_PENALTY}
- name: ${AIDER_EDITOR_MODEL}
  edit_format: diff
  use_repo_map: true
  examples_as_sys_msg: true
  extra_params:
    max_tokens: 8192
    temperature: ${AIDER_TEMPERATURE}
    frequency_penalty: ${AIDER_FREQUENCY_PENALTY}
EOF

# Architect mode + explicit files + a real test loop. AIDER_RESTORE keeps the
# conversation across rounds so fixes build on prior context instead of starting
# cold every time. Each round is bounded by AIDER_TIMEOUT.
#
# IMPORTANT: restored chat history brings back the conversation TEXT, not the
# editable files — a fresh aider process starts with an empty file set. Every
# follow-up round must therefore re-add the files it should edit, or the editor
# model refuses ("no files provided") and the round silently produces nothing.
AIDER_RESTORE="no"

# Files touched on the branch so far (excluding deletions) — what follow-up
# rounds need in the chat, since findings refer to the diff.
branch_files() {
  git diff --name-only --diff-filter=d "origin/${BASE_BRANCH}...HEAD" 2>/dev/null
}
run_aider() {
  # run_aider MESSAGE_FILE [FILE...]
  local msg="$1"; shift
  local extra=()
  [ "$AIDER_RESTORE" = "yes" ] && extra+=(--restore-chat-history)
  if [ -n "$SCOPE_TEST_CMD" ]; then
    extra+=(--auto-test --test-cmd "$SCOPE_TEST_CMD")
  fi
  timeout --signal=TERM --kill-after=30 "$AIDER_TIMEOUT" aider \
    --model "$AIDER_MODEL" \
    --architect \
    --editor-model "$AIDER_EDITOR_MODEL" \
    --auto-accept-architect \
    --model-settings-file /work/aider-model-settings.yml \
    --map-tokens "$AIDER_MAP_TOKENS" \
    --yes-always \
    --no-check-update \
    --no-gitignore \
    --no-attribute-author \
    --no-attribute-committer \
    --message-file "$msg" \
    ${extra[@]+"${extra[@]}"} \
    "$@"
  local rc=$?
  AIDER_RESTORE="yes"
  if [ "$rc" -eq 124 ]; then
    echo "=== aider round exceeded ${AIDER_TIMEOUT}s and was killed ==="
  fi
  return $rc
}

# Count of feature commits on the branch (i.e. work the model has actually
# committed). Architect mode commits incrementally, so a killed round can still
# have landed useful work.
commit_count() {
  git rev-list --count "origin/${BASE_BRANCH}..HEAD" 2>/dev/null || echo 0
}

# A corrective aider round that is ALLOWED to time out: rc 124 (killed mid-round)
# is logged and swallowed so the surrounding gate can re-evaluate whatever got
# committed instead of discarding the whole run. A genuine aider crash (any other
# non-zero rc) still fails the job.
run_aider_round() {
  run_aider "$@"
  local rc=$?
  case "$rc" in
    0|124) return 0 ;;
    *) return "$rc" ;;
  esac
}

echo "=== running aider (architect=${AIDER_MODEL}, editor=${AIDER_EDITOR_MODEL}) ==="
{
  printf '%s' "$AGENT_PROMPT"
  if [ -n "$SCOPE_NOTES" ]; then
    printf '\n\nGuidance from repository analysis:\n%s\n' "$SCOPE_NOTES"
  fi
  if [ -n "$SCOPE_TEST_CMD" ]; then
    printf '\nThe change is validated with: %s\n' "$SCOPE_TEST_CMD"
  fi
} > /work/prompt.txt
# The main implementation round. A timeout here must NOT throw away work: architect
# mode commits as it goes, so a round killed during a late reflection can already
# hold the whole feature. On 124 we continue to the gates (which re-check
# everything) as long as SOMETHING was committed; only a timeout with zero commits,
# or a real aider crash, fails the job.
run_aider /work/prompt.txt ${SCOPE_FILES[@]+"${SCOPE_FILES[@]}"}
aider_rc=$?
if [ "$aider_rc" -eq 124 ]; then
  if [ "$(commit_count)" -gt 0 ]; then
    echo "=== aider timed out but committed $(commit_count) commit(s); continuing to the gates ==="
  else
    fail "aider timed out before committing any work"
  fi
elif [ "$aider_rc" -ne 0 ]; then
  fail "aider run failed (rc=${aider_rc})"
fi

# A change must ship a test (the prompt demands it). Repos are polyglot, so we
# check common test path shapes rather than a language-specific test command.
tests_changed() {
  git diff --name-only "origin/${BASE_BRANCH}..HEAD" \
    | grep -Eiq '(^|/)(tests?|__tests__|spec)/|(_test\.|\.test\.|\.spec\.|Test\.|_spec\.)'
}

if ! tests_changed; then
  echo "=== no test detected; asking aider to add one ==="
  cat > /work/test-required.txt <<'EOF'
Your change does not add or modify any automated test. Add at least one test
that exercises the behaviour you just implemented and would FAIL if your change
were reverted. Match the repository's existing test framework and layout. Then
make sure the project still builds.
EOF
  mapfile -t ROUND_FILES < <(branch_files)
  run_aider_round /work/test-required.txt ${ROUND_FILES[@]+"${ROUND_FILES[@]}"} \
    || fail "aider test-adding run failed"
fi

# Guard against a "no real work" run. --no-gitignore already suppresses aider's
# ".aider* added to .gitignore" housekeeping commit; also reject a diff that
# touched ONLY .gitignore, which would otherwise pass the raw commit count and
# waste every downstream gate round.
meaningful=$(git diff --name-only "origin/${BASE_BRANCH}..HEAD" | grep -vc '^\.gitignore$' || true)
if [ "${meaningful:-0}" -eq 0 ]; then
  fail "the coding agent made no meaningful commits"
fi

# --- pre-PR self-review ----------------------------------------------------------
# Cheap gate that catches the worst failure mode BEFORE burning a PR + external
# review round: the agent implementing something unrelated to the request. The
# judge model reads the actual diff; on "not implemented" we hand its critique
# back to aider for a corrective round. Fails OPEN after SELF_REVIEW_ROUNDS —
# the external review loop is still behind us.
self_review() {
  SELF_OK="no"
  SELF_CRITIQUE=""
  local diff sys user content json
  # Drop generated/lockfiles and lead with the --stat so a big regenerated lockfile
  # (often alphabetically first) can't crowd the real change out from under the byte
  # cap and trigger a false "not implemented".
  local exclude=(':(exclude)package-lock.json' ':(exclude)yarn.lock'
    ':(exclude)pnpm-lock.yaml' ':(exclude)go.sum' ':(exclude)*.lock'
    ':(exclude)*.min.js')
  diff=$(
    printf 'FILES CHANGED:\n'
    git diff --stat "origin/${BASE_BRANCH}..HEAD" -- . "${exclude[@]}"
    printf '\nDIFF (generated/lockfiles omitted):\n'
    git diff "origin/${BASE_BRANCH}..HEAD" -- . "${exclude[@]}"
  )
  diff=$(printf '%s' "$diff" | head -c 60000)
  sys='You are a strict code reviewer. Given a feature request and the full diff of an automated agent'"'"'s change, judge whether the diff is ready to open as a pull request. Reply with ONLY a compact JSON object: {"implements":true|false,"critique":"<short: what is wrong and WHERE — name the two places that disagree>"}. Answer false if ANY of these hold: (a) the diff does not implement the requested behaviour in the right place, or changes unrelated code, or only partially implements it; (b) there is no meaningful automated test, OR the test does not exercise the real production code path it claims to (e.g. it re-implements the logic inline or asserts on a fixture instead of calling the actual function/endpoint) — a test that would still pass if the production change were reverted does not count; (c) the change spans layers whose contract is now inconsistent — for example the frontend reads field names the backend does not serialize (or vice versa), an API caller and its handler disagree on the request/response shape, or a constant/enum is consumed with a value the other side never produces. When unsure, answer false and say which two places to reconcile.'
  user=$(printf 'FEATURE REQUEST:\n%s\n\n%s\n' \
    "${AGENT_FEATURE:-$AGENT_PROMPT}" "$diff")
  content=$(deepseek_call "$sys" "$user") || return 0
  json=$(printf '%s' "$content" | extract_json)
  [ -n "$json" ] || return 0
  if [ "$(echo "$json" | jq -r '.implements // false' 2>/dev/null)" = "true" ]; then
    SELF_OK="yes"
  fi
  SELF_CRITIQUE=$(echo "$json" | jq -r '.critique // ""' 2>/dev/null)
}

self_round=0
while [ "$self_round" -lt "$SELF_REVIEW_ROUNDS" ]; do
  echo "=== pre-PR self-review (${REVIEW_JUDGE_MODEL}) ==="
  self_review
  if [ "$SELF_OK" = "yes" ]; then
    echo "=== self-review passed ==="
    break
  fi
  self_round=$((self_round + 1))
  echo "=== self-review: NOT implemented — ${SELF_CRITIQUE:-no critique} (corrective round ${self_round}/${SELF_REVIEW_ROUNDS}) ==="
  [ -n "$SELF_CRITIQUE" ] || break   # no critique to act on; fail open to the PR
  {
    echo "An independent review of your diff concluded the feature request is NOT"
    echo "correctly implemented yet. Its critique:"
    echo
    printf '%s\n' "$SELF_CRITIQUE"
    echo
    echo "Re-read the original request, revert/replace any unrelated changes, and"
    echo "implement the requested behaviour where it belongs — including a test."
  } > /work/self-review.txt
  mapfile -t ROUND_FILES < <(branch_files)
  run_aider_round /work/self-review.txt ${ROUND_FILES[@]+"${ROUND_FILES[@]}"} ${SCOPE_FILES[@]+"${SCOPE_FILES[@]}"} \
    || fail "aider self-review fix run failed"
done

# --- pre-PR verification gate --------------------------------------------------
# The authoritative local gate. Runs the repo's real build/lint/test command (the
# same one CI runs) plus its pre-commit hooks BEFORE the PR opens. A lint error or
# build break caught here becomes a cheap local fix round; the alternative is a
# failed CI check that blocks merge and burns review rounds. If the code cannot be
# made green in VERIFY_MAX_ROUNDS rounds we FAIL the job and open no PR — shipping a
# red PR that can never merge is strictly worse.
#
# The effective command is the repo-configured VERIFY_CMD, falling back to the
# detected SCOPE_TEST_CMD (already baseline-green) so even unconfigured repos get
# one final check of the pushed diff.
EFFECTIVE_VERIFY="${VERIFY_CMD:-$SCOPE_TEST_CMD}"

have_precommit() {
  [ -f .pre-commit-config.yaml ] && command -v pre-commit >/dev/null 2>&1
}

# Run every configured verification step; combined output -> /work/verify.log.
# Returns non-zero if any step fails.
run_verify() {
  : > /work/verify.log
  local rc=0
  if have_precommit; then
    local changed=()
    mapfile -t changed < <(git diff --name-only "origin/${BASE_BRANCH}..HEAD")
    if [ "${#changed[@]}" -gt 0 ]; then
      echo "=== pre-commit run (changed files) ===" | tee -a /work/verify.log
      pre-commit run --files "${changed[@]}" >>/work/verify.log 2>&1 || rc=$?
    fi
  fi
  if [ -n "$EFFECTIVE_VERIFY" ]; then
    echo "=== verify: ${EFFECTIVE_VERIFY} ===" | tee -a /work/verify.log
    bash -c "$EFFECTIVE_VERIFY" >>/work/verify.log 2>&1 || rc=$?
  fi
  return $rc
}

# One verification attempt. Auto-fixing hooks (formatters, eslint --fix) commonly
# rewrite files and still exit non-zero on the same run; commit those and re-check
# once for free before spending an aider round on them.
verify_once() {
  run_verify && return 0
  if ! git diff --quiet || ! git diff --cached --quiet; then
    echo "=== verify auto-fixed files; committing and re-checking ==="
    git add -A
    git commit -m "chore: apply verify auto-fixes" --no-verify >/dev/null 2>&1 || true
    run_verify && return 0
  fi
  return 1
}

# Install node deps for every tracked package.json (excluding node_modules). The
# test-command prep only covers the dirs that command touches, but the repo's
# pre-commit hooks (eslint/tsc/vitest) can run in other packages and fail with
# "command not found" (exit 127) when their node_modules are absent. Best-effort:
# a failed install is logged, not fatal.
install_repo_node_deps() {
  local pj dir
  while IFS= read -r pj; do
    dir=$(dirname "$pj")
    prepare_npm_dir "$dir" || echo "=== npm install failed in ${dir}; pre-commit hooks there may not run ==="
  done < <(git ls-files | grep -E '(^|/)package\.json$' | grep -v '/node_modules/')
}

verify_gate() {
  if [ -z "$EFFECTIVE_VERIFY" ] && ! have_precommit; then
    echo "=== no verify command or pre-commit config; skipping local gate ==="
    return 0
  fi
  # pre-commit hooks may shell out to per-package JS tooling; make sure it is
  # installed before the gate runs, or those hooks fail with exit 127.
  if have_precommit; then
    echo "=== ensuring node deps for pre-commit hooks ==="
    install_repo_node_deps
  fi
  echo "=== pre-PR verification gate (cmd='${EFFECTIVE_VERIFY:-none}', precommit=$(have_precommit && echo yes || echo no)) ==="
  verify_once && { echo "=== local verification passed ==="; return 0; }
  local vround=0
  while [ "$vround" -lt "$VERIFY_MAX_ROUNDS" ]; do
    vround=$((vround + 1))
    echo "=== verification failing; handing to aider (round ${vround}/${VERIFY_MAX_ROUNDS}) ==="
    {
      echo "Your change FAILS the repository's local verification — the same build/lint/"
      echo "test checks CI runs. Fix every failure below. Keep the feature and its tests"
      echo "intact and do not introduce unrelated changes. Command output:"
      echo
      tail -c 12000 /work/verify.log
    } > /work/verify-findings.txt
    mapfile -t ROUND_FILES < <(branch_files)
    run_aider_round /work/verify-findings.txt ${ROUND_FILES[@]+"${ROUND_FILES[@]}"} ${SCOPE_FILES[@]+"${SCOPE_FILES[@]}"} \
      || fail "aider verify-fix run failed"
    verify_once && { echo "=== local verification passed after fix round ${vround} ==="; return 0; }
  done
  echo "=== local verification still failing after ${VERIFY_MAX_ROUNDS} round(s) ==="
  tail -40 /work/verify.log
  return 1
}

if ! verify_gate; then
  # Don't discard a near-complete change. Push the branch (no PR — it can't pass
  # CI yet) so a human can finish it, and say so in the reason. The branch is the
  # bot's own; this is the same push that would happen had verification passed.
  reason="local verification (build/lint/test) still failing after ${VERIFY_MAX_ROUNDS} fix round(s); no PR opened"
  echo "=== ${reason}; pushing branch for manual completion ==="
  if git push origin "$AGENT_BRANCH" >/dev/null 2>&1; then
    reason="${reason}; branch ${AGENT_BRANCH} pushed for manual completion"
  else
    reason="${reason}; branch push also failed"
  fi
  emit_result "$(jq -cn --arg r "$reason" --arg b "$AGENT_BRANCH" \
    '{status:"failed", reason:$r, branch:$b, merged:false}')"
  exit 1
fi

echo "=== pushing branch ${AGENT_BRANCH} ==="
git push origin "$AGENT_BRANCH" || fail "git push failed"

echo "=== opening pull request ==="
PR_BODY="Automated change created by the coding-agent platform.

Feature request:

$(printf '%s' "${AGENT_FEATURE:-see commit history}" | head -c 2000)

Implemented by aider + ${AIDER_MODEL}. This PR will be reviewed by the
repository's GitHub Action; findings are addressed automatically before merge."

# gh_api returns non-zero on an HTTP error but still echoes the body; don't mask
# it with a generic message — the specific check below reports GitHub's own reason.
PR_RESPONSE=$(gh_api POST "${API}/pulls" "$(jq -cn \
  --arg title "$AGENT_PR_TITLE" \
  --arg head "$AGENT_BRANCH" \
  --arg base "$BASE_BRANCH" \
  --arg body "$PR_BODY" \
  '{title:$title, head:$head, base:$base, body:$body}')")

PR_NUMBER=$(echo "$PR_RESPONSE" | jq -r '.number // empty')
PR_URL=$(echo "$PR_RESPONSE" | jq -r '.html_url // empty')
[ -n "$PR_NUMBER" ] || fail "PR creation failed: $(echo "$PR_RESPONSE" | jq -r '.message // "unknown error"')"
echo "=== opened PR #${PR_NUMBER}: ${PR_URL} ==="

# --- review -> fix loop ------------------------------------------------------
# The AI reviewer (oglimmer/review-action) keeps its whole verdict in ONE sticky
# PR conversation comment, edited in place on every push and tagged with a hidden
# marker. It posts a formal PR review object only when it has inline findings, and
# even then the review body is just the marker — so the reviews endpoint is not a
# reliable "did the reviewer respond" signal or a source of prose. We key off the
# sticky comment instead. The marker below matches the string the action emits on
# its summary comment; inline-comment markers are stripped generically on render.
REVIEW_SUMMARY_MARKER='<!-- openai-pr-review-action -->'

# The reviewer's summary, as plain prose. Reads the sticky conversation comment
# (issues endpoint), takes the latest one carrying the summary marker, and strips
# any hidden HTML-comment markers the action embeds.
fetch_review_summary() {
  gh_api GET "${API}/issues/${PR_NUMBER}/comments?per_page=100" \
    | jq -r --arg m "$REVIEW_SUMMARY_MARKER" \
        '[.[] | select((.body // "") | contains($m))] | last
         | (.body // "(no summary)") | gsub("<!--[^>]*-->"; "") | gsub("^\\s+|\\s+$"; "")'
}

# Wait until the head commit's check runs are complete AND the reviewer has
# responded (its sticky summary comment is present) — or a check has already
# failed, which is actionable on its own. We must not hang waiting for a formal
# verdict that this reviewer does not emit. Returns via globals:
#   REVIEW_STATE  "APPROVED" | "CHANGES_REQUESTED" | ""  (formal verdict, if any)
#   REVIEW_SEEN   "yes" | "no"                            (reviewer responded at all)
#   FAILED_CHECKS count of failing/blocking check runs
wait_for_review() {
  local head_sha="$1"
  local deadline=$(( $(date +%s) + REVIEW_TIMEOUT ))
  local iters=0
  while [ "$(date +%s)" -lt "$deadline" ]; do
    iters=$((iters + 1))
    local checks reviews comments total completed seen
    # A failed fetch means "unknown, poll again" — never "no checks / no review".
    checks=$(gh_api GET "${API}/commits/${head_sha}/check-runs?per_page=100") \
      || { echo "=== check-runs fetch failed; retrying ==="; sleep "$REVIEW_POLL"; continue; }
    total=$(echo "$checks" | jq -r '.check_runs | length // 0')
    completed=$(echo "$checks" | jq -r '[.check_runs[]? | select(.status=="completed")] | length')
    FAILED_CHECKS=$(echo "$checks" | jq -r \
      '[.check_runs[]? | select(.conclusion=="failure" or .conclusion=="timed_out" or .conclusion=="cancelled" or .conclusion=="action_required")] | length')

    reviews=$(gh_api GET "${API}/pulls/${PR_NUMBER}/reviews?per_page=100") \
      || { echo "=== reviews fetch failed; retrying ==="; sleep "$REVIEW_POLL"; continue; }
    # Formal verdict, only when it belongs to the CURRENT head commit. The reviewer
    # posts a formal review object only when it has inline findings; after a clean
    # fix it merely edits its sticky comment, leaving the old CHANGES_REQUESTED as
    # the "last" review forever. Scoping to head_sha lets a resolved verdict fall
    # through to the prose judge instead of pinning the decision to "fix".
    REVIEW_STATE=$(echo "$reviews" \
      | jq -r --arg bot "$BOT_LOGIN" --arg sha "$head_sha" \
        '[.[] | select(.user.login != $bot) | select(.commit_id == $sha)
              | select(.state=="APPROVED" or .state=="CHANGES_REQUESTED")] | last | .state // ""')

    comments=$(gh_api GET "${API}/issues/${PR_NUMBER}/comments?per_page=100") \
      || { echo "=== comments fetch failed; retrying ==="; sleep "$REVIEW_POLL"; continue; }
    # Whether the reviewer has weighed in on THIS push. The sticky summary comment
    # is edited in place across rounds, so its mere presence is stale — require it
    # to have been updated after we pushed the commit under review (REVIEW_SINCE).
    # (Worker/GitHub clock skew is negligible next to the check+review latency
    # between our push and the reviewer editing its comment.)
    seen=$(echo "$comments" \
      | jq -r --arg m "$REVIEW_SUMMARY_MARKER" --arg since "$REVIEW_SINCE" \
        '[.[] | select((.body // "") | contains($m))
              | select(($since == "") or (.updated_at > $since))] | length')
    REVIEW_SEEN="no"; [ "${seen:-0}" -gt 0 ] && REVIEW_SEEN="yes"

    # "done" = all present checks completed. total==0 is ambiguous: either the repo
    # has no checks, or checks for a just-pushed fix commit have not registered yet.
    # Only accept total==0 after a short grace so we never merge a fix before CI reruns.
    local checks_done="no"
    if [ "$total" -gt 0 ] && [ "$completed" -eq "$total" ]; then
      checks_done="yes"
    elif [ "$total" -eq 0 ] && [ "$iters" -ge "$NO_CHECK_GRACE" ]; then
      checks_done="yes"
    fi

    if [ "$checks_done" = "yes" ] && { [ "$REVIEW_SEEN" = "yes" ] || [ "$FAILED_CHECKS" -gt 0 ]; }; then
      echo "=== review ready: verdict='${REVIEW_STATE:-none}' reviewed=${REVIEW_SEEN} failed_checks=${FAILED_CHECKS} ==="
      return 0
    fi
    echo "=== waiting for review (checks ${completed}/${total}, verdict='${REVIEW_STATE:-none}', reviewed=${REVIEW_SEEN}) ==="
    sleep "$REVIEW_POLL"
  done
  REVIEW_STATE=""
  REVIEW_SEEN="no"
  FAILED_CHECKS=0
  return 1
}

# Real detail for each failed check: its GitHub summary, its annotations
# (path:line: message), and a tail of the matching Actions job log — otherwise the
# model gets only a bare "check: failed" and fixes CI blind. The check-run id is
# the Actions job id for workflow-produced checks; other providers 404 and degrade
# to summary+annotations. `curl -L` drops the Authorization header on the cross-host
# redirect to blob storage, so the token is not leaked to the log-download host.
failed_check_details() {
  local head_sha="$1" checks id name
  checks=$(gh_api GET "${API}/commits/${head_sha}/check-runs?per_page=100") || return 0
  while IFS=$'\t' read -r id name; do
    [ -n "$id" ] || continue
    echo "### CHECK: ${name}"
    echo "$checks" | jq -r --arg id "$id" \
      '.check_runs[]? | select((.id|tostring)==$id) | .output.summary // .output.title // "failed"'
    gh_api GET "${API}/check-runs/${id}/annotations?per_page=50" 2>/dev/null \
      | jq -r '.[]? | "  \(.path):\(.start_line // "?"): \(.annotation_level // "note"): \((.message // "")|gsub("\n";" "))"' 2>/dev/null
    local logtail
    logtail=$(curl -sSL --max-time 30 \
      -H "Authorization: Bearer ${GITHUB_TOKEN}" \
      -H "Accept: application/vnd.github+json" \
      "${API}/actions/jobs/${id}/logs" 2>/dev/null | tail -c 4000)
    if [ -n "$logtail" ]; then
      echo "  --- job log tail ---"
      printf '%s\n' "$logtail" | sed 's/^/  /'
    fi
  done < <(echo "$checks" | jq -r '.check_runs[]?
      | select(.conclusion=="failure" or .conclusion=="timed_out" or .conclusion=="cancelled")
      | "\(.id)\t\(.name)"')
}

collect_findings() {
  local head_sha="$1"
  {
    echo "The pull request has unresolved findings from CI and/or the reviewer."
    echo "Address every finding below (fix failing checks first), keep the change"
    echo "minimal, keep tests passing, and do not introduce unrelated modifications."
    echo
    echo "--- REVIEW SUMMARY ---"
    # The reviewer's prose lives in its sticky summary comment, not the review body.
    fetch_review_summary
    echo
    echo "--- INLINE COMMENTS ---"
    gh_api GET "${API}/pulls/${PR_NUMBER}/comments?per_page=100" \
      | jq -r '.[]? | "\(.path):\(.line // .original_line // "?"): \((.body // "") | gsub("<!--[^>]*-->"; "") | gsub("^\\s+|\\s+$"; ""))"'
    echo
    echo "--- FAILED CHECKS ---"
    failed_check_details "$head_sha" | head -c 20000
  } > /work/findings.txt
}

# The reviewer's latest body + inline comments as plain text (for the LLM judge).
gather_review_text() {
  echo "REVIEW BODY:"
  fetch_review_summary
  echo
  echo "INLINE COMMENTS:"
  gh_api GET "${API}/pulls/${PR_NUMBER}/comments?per_page=100" \
    | jq -r '.[]? | "- \(.path):\(.line // .original_line // "?"): \((.body // "") | gsub("<!--[^>]*-->"; "") | gsub("^\\s+|\\s+$"; ""))"'
}

# Classify whether a prose review approves the change. Sets JUDGE_VERDICT
# ("approve"|"needs_changes") and JUDGE_REASON. Fails SAFE to needs_changes on any
# error or ambiguity so an unread negative review can never be merged past.
judge_review() {
  JUDGE_VERDICT="needs_changes"
  JUDGE_REASON=""
  local sys user content lc
  sys='You decide whether an automated code review approves merging a pull request. The PR must actually implement the requested feature AND ship a test for it. Reply with ONLY a compact JSON object: {"verdict":"approve"|"needs_changes","reason":"<short>"}. Use "needs_changes" if the review indicates the feature is unmet, incomplete, or incorrect, that unrelated code was changed, that tests are missing, or if it requests any change. Use "approve" only when the review raises no blocking concerns (minor optional nits are fine). When in doubt, answer "needs_changes".'
  user=$(printf 'FEATURE REQUESTED:\n%s\n\nAUTOMATED REVIEW:\n%s\n' \
    "${AGENT_FEATURE:-(not provided)}" "$(gather_review_text)")

  content=$(deepseek_call "$sys" "$user") || { JUDGE_REASON="review-judge request failed"; return; }
  if [ -z "$content" ]; then
    JUDGE_REASON="review-judge returned no content"
    return
  fi
  lc=$(printf '%s' "$content" | tr '[:upper:]' '[:lower:]')
  # Prefer the safe verdict: any mention of needs_changes wins.
  if printf '%s' "$lc" | grep -q 'needs_changes'; then
    JUDGE_VERDICT="needs_changes"
  elif printf '%s' "$lc" | grep -qE '"verdict"[^}]*approve|^[[:space:]]*approve'; then
    JUDGE_VERDICT="approve"
  fi
  JUDGE_REASON=$(printf '%s' "$content" | jq -r '.reason // empty' 2>/dev/null)
  [ -n "$JUDGE_REASON" ] || JUDGE_REASON=$(printf '%s' "$content" | tr '\n' ' ' | cut -c1-200)
}

REVIEW_STATE=""
REVIEW_SEEN="no"
FAILED_CHECKS=0
JUDGE_VERDICT=""
JUDGE_REASON=""
MERGED=false
# UTC timestamp of the most recent push; a sticky review comment is only "fresh"
# once its updated_at passes this. Set immediately before every push.
REVIEW_SINCE=""

# Evaluate the head commit up to REVIEW_MAX_ROUNDS+1 times but only spend a fix in
# the first REVIEW_MAX_ROUNDS iterations: this way the LAST pushed fix still gets a
# wait+decide (and can merge) instead of being pushed and abandoned.
for attempt in $(seq 0 "$REVIEW_MAX_ROUNDS"); do
  HEAD_SHA=$(git rev-parse HEAD)
  if ! wait_for_review "$HEAD_SHA"; then
    fail "timed out waiting for the PR review"
  fi

  # Decide merge vs. fix. A failing check always means fix. Otherwise honour the
  # review verdict: APPROVED merges, CHANGES_REQUESTED fixes. When the reviewer
  # gave no formal verdict (a COMMENTED review — common for AI reviewers whose
  # judgment is prose), classify that prose with the model and fail SAFE: only an
  # explicit "approve" merges, anything else fixes.
  decision="fix"
  if [ "$FAILED_CHECKS" -eq 0 ]; then
    case "$REVIEW_STATE" in
      APPROVED) decision="merge" ;;
      CHANGES_REQUESTED) decision="fix" ;;
      *)
        echo "=== no formal verdict; classifying review prose (${REVIEW_JUDGE_MODEL}) ==="
        judge_review
        echo "=== review judge: ${JUDGE_VERDICT} — ${JUDGE_REASON} ==="
        [ "$JUDGE_VERDICT" = "approve" ] && decision="merge"
        ;;
    esac
  fi

  if [ "$decision" = "merge" ]; then
    echo "=== approved for merge (checks green, verdict='${REVIEW_STATE:-none}'); merging ==="
    MERGE_RESPONSE=$(gh_api PUT "${API}/pulls/${PR_NUMBER}/merge" \
      "$(jq -cn --arg m "squash" '{merge_method:$m}')")
    if [ "$(echo "$MERGE_RESPONSE" | jq -r '.merged // false')" = "true" ]; then
      MERGED=true
      break
    fi
    fail "auto-merge failed: $(echo "$MERGE_RESPONSE" | jq -r '.message // "unknown error"')"
  fi

  # Not mergeable, and no fix budget left: the fix pushed last round was just
  # evaluated above and still fell short. Stop and leave the PR open.
  if [ "$attempt" -ge "$REVIEW_MAX_ROUNDS" ]; then
    break
  fi

  fix_round=$((attempt + 1))
  echo "=== findings to address (failed_checks=${FAILED_CHECKS}, verdict='${REVIEW_STATE:-none}'; round ${fix_round}/${REVIEW_MAX_ROUNDS}) ==="
  collect_findings "$HEAD_SHA"
  mapfile -t ROUND_FILES < <(branch_files)
  run_aider_round /work/findings.txt ${ROUND_FILES[@]+"${ROUND_FILES[@]}"} ${SCOPE_FILES[@]+"${SCOPE_FILES[@]}"} \
    || fail "aider fix run failed"

  if [ "$(git rev-list --count "origin/${AGENT_BRANCH}..HEAD")" -eq 0 ]; then
    fail "agent produced no fix commits for the review findings"
  fi
  REVIEW_SINCE=$(date -u +%Y-%m-%dT%H:%M:%SZ)   # freshness baseline for this push
  git push origin "$AGENT_BRANCH" || fail "git push (fix) failed"
done

if [ "$MERGED" != "true" ]; then
  reason="review not approved after ${REVIEW_MAX_ROUNDS} round(s); PR left open"
  [ -n "$JUDGE_REASON" ] && reason="${reason} (last review verdict: ${JUDGE_VERDICT} — ${JUDGE_REASON})"
  emit_result "$(jq -cn --arg u "$PR_URL" --arg b "$AGENT_BRANCH" --arg r "$reason" \
    '{status:"failed", pr_url:$u, branch:$b, merged:false, reason:$r}')"
  exit 1
fi

echo "=== done: merged ${PR_URL} ==="
emit_result "$(jq -cn --arg u "$PR_URL" --arg b "$AGENT_BRANCH" \
  '{status:"success", pr_url:$u, branch:$b, merged:true}')"
