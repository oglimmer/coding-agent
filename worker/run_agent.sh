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
#   REVIEW_JUDGE_MODEL  model for scoping/self-review/review-judging (default: deepseek-chat)
#   SELF_REVIEW_ROUNDS  pre-PR corrective rounds, default: 2
#   AIDER_TIMEOUT       seconds per aider round before it is killed (default: 1800)
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
SELF_REVIEW_ROUNDS="${SELF_REVIEW_ROUNDS:-2}"
# Hard per-round bound: a model stuck in a repetition loop must not ride out the
# whole Job deadline.
AIDER_TIMEOUT="${AIDER_TIMEOUT:-1800}"
# DeepSeek degenerates into verbatim repetition loops at temperature 0 (aider's
# default) on long generations; a little temperature + frequency penalty is the
# standard countermeasure.
AIDER_TEMPERATURE="${AIDER_TEMPERATURE:-0.2}"
AIDER_FREQUENCY_PENALTY="${AIDER_FREQUENCY_PENALTY:-0.3}"
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
  # gh_api METHOD URL [DATA]
  local method="$1" url="$2" data="${3:-}"
  if [ -n "$data" ]; then
    curl -sS -X "$method" \
      -H "Authorization: Bearer ${GITHUB_TOKEN}" \
      -H "Accept: application/vnd.github+json" \
      "$url" -d "$data"
  else
    curl -sS -X "$method" \
      -H "Authorization: Bearer ${GITHUB_TOKEN}" \
      -H "Accept: application/vnd.github+json" \
      "$url"
  fi
}

# deepseek_call SYSTEM USER — one-shot chat completion against the helper model;
# echoes the assistant content, empty output on any failure (callers fail open
# or fail safe as appropriate to their gate).
deepseek_call() {
  local sys="$1" user="$2" payload resp
  payload=$(jq -cn --arg m "$REVIEW_JUDGE_MODEL" --arg s "$sys" --arg u "$user" \
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

# Only accept a model-proposed test command that is trivially auditable: segments
# joined by '&&', each starting with a known build tool that exists in the image.
# (The repo's own test suite runs arbitrary code anyway — this guards against
# nonsense, not malice.)
validate_test_cmd() {
  [ -n "$SCOPE_TEST_CMD" ] || return 0
  # shellcheck disable=SC2016  # literal '$(' pattern match, not an expansion
  case "$SCOPE_TEST_CMD" in
    *'|'*|*';'*|*'`'*|*'$('*|*'>'*|*'<'*) SCOPE_TEST_CMD=""; return 0 ;;
  esac
  local seg tok
  while IFS= read -r seg; do
    seg="${seg#"${seg%%[![:space:]]*}"}"
    tok="${seg%% *}"
    case "$tok" in
      cd) ;;
      go|npm|npx|node|python|python3|pytest|make)
        command -v "$tok" >/dev/null 2>&1 || { SCOPE_TEST_CMD=""; return 0; } ;;
      *) SCOPE_TEST_CMD=""; return 0 ;;
    esac
  done < <(printf '%s' "$SCOPE_TEST_CMD" | sed 's/&&/\n/g')
}

scope_repo() {
  local listing content json sys user
  listing=$(git ls-files | head -3000)
  sys='You prepare a coding task for an automated coding agent. Given a feature request and the repository file listing, reply with ONLY a compact JSON object: {"files":["path",...],"test_cmd":"...","notes":"..."}. "files": 5-15 EXISTING paths from the listing that are most relevant — the files that must change, plus key context (routing, templates, similar features, the tests to mirror). "test_cmd": the single fastest shell command that validates a change in the affected area, using only go/npm/npx/node/python/pytest/make, segments joined by &&, "cd <dir> && ..." allowed (e.g. "cd backend && go test ./..."); empty string if unsure. "notes": 2-4 sentences on WHERE the feature belongs and how to implement it idiomatically in this codebase.'
  user=$(printf 'FEATURE REQUEST:\n%s\n\nREPOSITORY FILE LISTING:\n%s\n' \
    "${AGENT_FEATURE:-$AGENT_PROMPT}" "$listing")
  content=$(deepseek_call "$sys" "$user") || return 0
  json=$(printf '%s' "$content" | extract_json) || return 0
  [ -n "$json" ] || return 0

  local f
  while IFS= read -r f; do
    [ -f "$f" ] && SCOPE_FILES+=("$f")
  done < <(echo "$json" | jq -r '.files[]? // empty' 2>/dev/null | head -15)
  SCOPE_TEST_CMD=$(echo "$json" | jq -r '.test_cmd // ""' 2>/dev/null)
  SCOPE_NOTES=$(echo "$json" | jq -r '.notes // ""' 2>/dev/null)
  validate_test_cmd
}

echo "=== scoping the task (${REVIEW_JUDGE_MODEL}) ==="
scope_repo
echo "=== scope: ${#SCOPE_FILES[@]} files, test_cmd='${SCOPE_TEST_CMD:-none}' ==="
[ -n "$SCOPE_NOTES" ] && echo "=== scope notes: ${SCOPE_NOTES} ==="

# The test command is only useful if it can actually run: install npm deps it
# needs, then require it to PASS on the untouched branch. A command that fails
# at baseline (missing deps, broken env) would send aider chasing phantom
# failures instead of the feature — exactly the confusion spiral we must avoid.
prepare_test_cmd() {
  [ -n "$SCOPE_TEST_CMD" ] || return 0
  if printf '%s' "$SCOPE_TEST_CMD" | grep -qE '(^|&& *)(npm|npx|node) '; then
    local dir="."
    case "$SCOPE_TEST_CMD" in
      "cd "*) dir=$(printf '%s' "$SCOPE_TEST_CMD" | sed -n 's/^cd \([^ ]*\) &&.*/\1/p'); [ -n "$dir" ] || dir="." ;;
    esac
    if [ -f "$dir/package.json" ] && [ ! -d "$dir/node_modules" ]; then
      echo "=== installing npm deps in ${dir} ==="
      ( cd "$dir" && { npm ci --no-audit --no-fund || npm install --no-audit --no-fund; } ) \
        >/work/npm-install.log 2>&1 \
        || { echo "=== npm install failed; disabling auto-test ==="; tail -5 /work/npm-install.log; SCOPE_TEST_CMD=""; return 0; }
    fi
  fi
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
run_aider /work/prompt.txt ${SCOPE_FILES[@]+"${SCOPE_FILES[@]}"} || fail "aider run failed"

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
  run_aider /work/test-required.txt ${ROUND_FILES[@]+"${ROUND_FILES[@]}"} \
    || fail "aider test-adding run failed"
fi

if [ "$(git rev-list --count "origin/${BASE_BRANCH}..HEAD")" -eq 0 ]; then
  fail "the coding agent made no commits"
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
  diff=$(git diff "origin/${BASE_BRANCH}..HEAD" | head -c 60000)
  sys='You are a strict code reviewer. Given a feature request and the full diff of an automated agent'"'"'s change, judge ONLY whether the diff actually implements the requested feature in the right place, with at least one meaningful automated test. Reply with ONLY a compact JSON object: {"implements":true|false,"critique":"<short: what is missing or wrong, and where the change should have been made>"}. Answer false if the diff changes unrelated code instead of the requested behaviour, only partially implements it, or tests are missing/meaningless.'
  user=$(printf 'FEATURE REQUEST:\n%s\n\nDIFF:\n%s\n' \
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
  run_aider /work/self-review.txt ${ROUND_FILES[@]+"${ROUND_FILES[@]}"} ${SCOPE_FILES[@]+"${SCOPE_FILES[@]}"} \
    || fail "aider self-review fix run failed"
done

echo "=== pushing branch ${AGENT_BRANCH} ==="
git push origin "$AGENT_BRANCH" || fail "git push failed"

echo "=== opening pull request ==="
PR_BODY="Automated change created by the coding-agent platform.

Feature request:

$(printf '%s' "${AGENT_FEATURE:-see commit history}" | head -c 2000)

Implemented by aider + ${AIDER_MODEL}. This PR will be reviewed by the
repository's GitHub Action; findings are addressed automatically before merge."

PR_RESPONSE=$(gh_api POST "${API}/pulls" "$(jq -cn \
  --arg title "$AGENT_PR_TITLE" \
  --arg head "$AGENT_BRANCH" \
  --arg base "$BASE_BRANCH" \
  --arg body "$PR_BODY" \
  '{title:$title, head:$head, base:$base, body:$body}')") || fail "GitHub PR API call failed"

PR_NUMBER=$(echo "$PR_RESPONSE" | jq -r '.number // empty')
PR_URL=$(echo "$PR_RESPONSE" | jq -r '.html_url // empty')
[ -n "$PR_NUMBER" ] || fail "PR creation failed: $(echo "$PR_RESPONSE" | jq -r '.message // "unknown error"')"
echo "=== opened PR #${PR_NUMBER}: ${PR_URL} ==="

# --- review -> fix loop ------------------------------------------------------
# Wait until the head commit's check runs are complete AND the reviewer has
# responded (a review of ANY state, including COMMENTED) — or a check has already
# failed, which is actionable on its own. Reviewers vary: some emit a formal
# APPROVED/CHANGES_REQUESTED verdict, many (e.g. GitHub Action reviewers) only post
# a COMMENTED review plus a pass/fail status check. We must not hang waiting for a
# verdict that will never come. Returns via globals:
#   REVIEW_STATE  "APPROVED" | "CHANGES_REQUESTED" | ""  (formal verdict, if any)
#   REVIEW_SEEN   "yes" | "no"                            (reviewer responded at all)
#   FAILED_CHECKS count of failing/blocking check runs
wait_for_review() {
  local head_sha="$1"
  local deadline=$(( $(date +%s) + REVIEW_TIMEOUT ))
  local iters=0
  while [ "$(date +%s)" -lt "$deadline" ]; do
    iters=$((iters + 1))
    local checks reviews total completed seen
    checks=$(gh_api GET "${API}/commits/${head_sha}/check-runs?per_page=100")
    total=$(echo "$checks" | jq -r '.check_runs | length // 0')
    completed=$(echo "$checks" | jq -r '[.check_runs[]? | select(.status=="completed")] | length')
    FAILED_CHECKS=$(echo "$checks" | jq -r \
      '[.check_runs[]? | select(.conclusion=="failure" or .conclusion=="timed_out" or .conclusion=="cancelled" or .conclusion=="action_required")] | length')

    reviews=$(gh_api GET "${API}/pulls/${PR_NUMBER}/reviews?per_page=100")
    # Formal verdict, if the reviewer emitted one.
    REVIEW_STATE=$(echo "$reviews" | jq -r --arg bot "$BOT_LOGIN" \
      '[.[] | select(.user.login != $bot) | select(.state=="APPROVED" or .state=="CHANGES_REQUESTED")] | last | .state // ""')
    # Whether the reviewer has weighed in at all — COMMENTED reviews count.
    seen=$(echo "$reviews" | jq -r --arg bot "$BOT_LOGIN" \
      '[.[] | select(.user.login != $bot)] | length')
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

collect_findings() {
  local head_sha="$1"
  {
    echo "The pull request has unresolved findings from CI and/or the reviewer."
    echo "Address every finding below (fix failing checks first), keep the change"
    echo "minimal, keep tests passing, and do not introduce unrelated modifications."
    echo
    echo "--- REVIEW SUMMARY ---"
    # Take the latest non-self review body regardless of state — this reviewer
    # posts COMMENTED reviews, not a formal CHANGES_REQUESTED verdict.
    gh_api GET "${API}/pulls/${PR_NUMBER}/reviews?per_page=100" \
      | jq -r --arg bot "$BOT_LOGIN" \
        '[.[] | select(.user.login != $bot) | select((.body // "") != "")] | last | .body // "(no summary)"'
    echo
    echo "--- INLINE COMMENTS ---"
    gh_api GET "${API}/pulls/${PR_NUMBER}/comments?per_page=100" \
      | jq -r '.[]? | "\(.path):\(.line // .original_line // "?"): \(.body)"'
    echo
    echo "--- FAILED CHECKS ---"
    gh_api GET "${API}/commits/${head_sha}/check-runs?per_page=100" \
      | jq -r '.check_runs[]? | select(.conclusion=="failure" or .conclusion=="timed_out" or .conclusion=="cancelled") | "\(.name): \(.output.summary // .output.title // "failed")"'
  } > /work/findings.txt
}

# The reviewer's latest body + inline comments as plain text (for the LLM judge).
gather_review_text() {
  echo "REVIEW BODY:"
  gh_api GET "${API}/pulls/${PR_NUMBER}/reviews?per_page=100" \
    | jq -r --arg bot "$BOT_LOGIN" \
      '[.[] | select(.user.login != $bot) | select((.body // "") != "")] | last | .body // "(no review body)"'
  echo
  echo "INLINE COMMENTS:"
  gh_api GET "${API}/pulls/${PR_NUMBER}/comments?per_page=100" \
    | jq -r '.[]? | "- \(.path):\(.line // .original_line // "?"): \(.body)"'
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

round=0
while [ "$round" -lt "$REVIEW_MAX_ROUNDS" ]; do
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

  round=$((round + 1))
  echo "=== findings to address (failed_checks=${FAILED_CHECKS}, verdict='${REVIEW_STATE:-none}'; round ${round}/${REVIEW_MAX_ROUNDS}) ==="
  collect_findings "$HEAD_SHA"
  mapfile -t ROUND_FILES < <(branch_files)
  run_aider /work/findings.txt ${ROUND_FILES[@]+"${ROUND_FILES[@]}"} || fail "aider fix run failed"

  if [ "$(git rev-list --count "origin/${AGENT_BRANCH}..HEAD")" -eq 0 ]; then
    fail "agent produced no fix commits for the review findings"
  fi
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
