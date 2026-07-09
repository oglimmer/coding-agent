# Worker hardening plan

Findings from a review of `worker/run_agent.sh` (worker pipeline) on 2026-07-08.
Goal: the worker must be strong enough to implement features AND reliably drive
them to a merged PR. Items are ordered by priority; each is independently
shippable. Line numbers refer to the state of `run_agent.sh` at commit `a50718a`
— re-locate by the quoted code if the file has moved on.

Legend: **Problem** what goes wrong · **Where** anchor · **Fix** approach ·
**Hints** implementation notes · **Verify** how to prove it works.

## Implementation status (branch `harden-worker`)

DONE (implemented + shellcheck-clean + unit-tested logic): 1.1, 1.2, 1.3, 1.4,
2.1, 2.2, 3.1, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 4.1, 4.3. Line anchors below
refer to the ORIGINAL `a50718a` layout and are now stale — see the current file.

Note on 3.5/3.6: those items hardened the *model-guessed* test command (validate
`npm run <script>`, add a compile/vet fallback). 3.8 supersedes their premise —
the worker no longer lets a model invent an executable test command at all — but
their guards (script existence check, fallback gate) are kept as belt-and-braces.

### 3.8 Detect the test command; never let a model invent one  ⬅ from job 4d3e5c04

Root cause of the `test:unit` class of failures: the scope model was asked to
*guess* `test_cmd`, and a guessed command is the only source that can name a
target that doesn't exist. Job `4d3e5c04` (repo `oglimmer/irl-planner-pro`) is the
canonical case — the model invented `npm run test:unit` (the frontend defines
`test`, not `test:unit`), baseline went red, and auto-test was disabled. Fixed by
removing the model as a source and detecting the command deterministically:
- **3.8a** Dropped `test_cmd` from the scope prompt/parsing and deleted
  `validate_test_cmd`. New `detect_test_cmd` builds `SCOPE_TEST_CMD` from real
  manifests: it maps `SCOPE_FILES` to their nearest enclosing module (`go.mod` /
  `package.json` / `pyproject.toml` / `Makefile`), reads each module's command
  from the manifest that is present (Go → `go test ./...`; Node → the first of
  `test`/`typecheck`/`build` actually in `.scripts`, skipping the npm-init
  placeholder; Python → `pytest`; Make → `make test`), and chains modules with
  correct relative `cd ../sibling` paths (`relpath`). Falls back to the repo's
  top-level modules when scoping returned nothing, so a failed scope still yields
  a signal. A hallucinated script name is now structurally impossible.
- **3.8b** Fixed a latent bug in `prepare_cmd_deps` that would have re-broken the
  exact cross-stack case: `while IFS= read -r seg` over the `&&`-split silently
  dropped the LAST segment (the split has no trailing newline). For a command like
  `cd backend && go test ./... && cd ../frontend && npm run test`, the final
  `npm run test` was never processed → frontend deps never installed and its
  script never checked → baseline red → auto-test disabled. Now
  `while IFS= read -r seg || [ -n "$seg" ]`.
- **3.8c** New optional per-repo `test_command` (env `AGENT_TEST_CMD`; DB
  `test_command`; surfaced in the repo add/edit form and job metadata), distinct
  from `verify_command`: it overrides detection for the fast aider `--auto-test`
  loop so an owner can supply a cheap loop command without making their heavy
  verify gate run after every edit. Empty = detect.
- **Precedence, both roles now deterministic (model never):** inner loop =
  `AGENT_TEST_CMD` › detection › compile/vet fallback (3.6); gate = `VERIFY_CMD` ›
  detected `SCOPE_TEST_CMD`.
- **Verify:** detection unit-tested against a real `irl-planner-pro` checkout
  (backend-only, frontend-only, both stacks, nested file, no-scope) → emits
  `npm run test`, not `test:unit`; `prepare_cmd_deps` reaches the frontend segment
  (installs deps, checks the script) and rejects a hallucinated last-segment
  script. Backend `go build`/`vet`/`test` and frontend `npm run check` green.

### 3.9 A hanging test/verify command kills the round with nothing committed  ⬅ from job 3d59189a

Root cause of the `aider timed out before committing any work` class: 3.8 made the
test command real and terminating-*by-name*, but never guaranteed it *terminates
at runtime*. Job `3d59189a` (repo `oglimmer/irl-planner-pro`) is canonical — the
detected inner-loop command `cd backend && go test ./... && cd ../frontend && npm
run test` ran, `go test` passed, then the frontend `npm run test` (vitest) hung
(`[vitest-pool]: Timeout terminating forks worker …`). aider's `--auto-test`
subprocess blocked on it, the whole 3600s `AIDER_TIMEOUT` round was hard-killed,
and because the one applied edit was never committed, 2.1's preserve-work salvage
had nothing to push → job failed, no PR. Session cost `$0.03`: almost the entire
hour was a wedged test process, not model work.
- **3.9a** New `bounded_runner RAW SECS`: writes RAW to a file (so the wrapper
  never re-quotes the compound `&&`/`cd`/embedded-quote command) and echoes
  `env CI=1 CI=true FORCE_COLOR=0 timeout --signal=TERM --kill-after=10 SECS bash <file>`.
  `CI=true` flips vitest/jest/react-scripts out of watch mode (the #1 non-return
  cause) and keeps npm non-interactive; `timeout` guarantees even a wedged process
  (a fork-teardown deadlock — which CI mode does NOT prevent) is killed and the
  runner exits non-zero. A hang therefore degrades to "this round's test failed",
  never a dead job. New tunables `AGENT_TEST_TIMEOUT` (inner loop, default 300s)
  and `AGENT_VERIFY_TIMEOUT` (heavy gate, default 1200s).
- **3.9b** Every execution site now runs through `bounded_runner`, while the
  parsing helpers (`prepare_cmd_deps`, `check_npm_script`) still see the RAW
  command: the inner-loop baseline check, the zero-config fallback-gate baseline,
  aider's `--test-cmd` (`SCOPE_TEST_RUNNER`, built once and reused across rounds),
  the `VERIFY_CMD` baseline check, and `run_verify`'s `EFFECTIVE_VERIFY`. A hang
  can no longer stall the job at setup, inside the aider loop, or at the gate.
- **Verify:** `bounded_runner` unit-tested — a `sleep 30` bounded to 2s returns
  rc 124 in ~2s; `CI` is exported to the command; a compound `cd a && … && cd
  ../b && false` passes through with cwd changes and propagates the non-zero exit;
  single/double quotes in the command survive; a fast command returns 0 promptly.
  shellcheck-clean.
- **Not fixed by 3.9 (the deeper issue):** this stack keeps dying in its own
  scaffolding rather than on code quality. 3.9 removes the hang failure mode but
  the `deepseek-v4-pro` + aider engine remains the real ceiling — see the
  engine-swap discussion (Claude Agent SDK / stronger AIDER_MODEL) as the
  higher-leverage change.

### 3.7 Cross-stack deps: cd-path bug + pre-commit tooling  ⬅ from job 874b4401

First post-fix run (`worker_commit c03a1ef`): the timeout/discard failure was gone
— aider ran to completion, the go build/vet fallback (3.6) gave it a backend
signal, and the branch was pushed for salvage (2.2). It then failed the verify
gate on `eslint --fix (frontend) ... Failed, exit code 127` (eslint not found:
frontend `node_modules` never installed). Two causes, both fixed:
- **3.7a** `prepare_cmd_deps` tracked only the last `cd` arg, so
  `cd backend && … && cd ../frontend` looked for `../frontend/package.json` from
  the repo root and skipped the frontend. Now the path accumulates
  (`backend/../frontend`, which the FS collapses to `frontend`).
- **3.7b** dep install was scoped to the TEST command's dirs, but the verify
  gate's pre-commit hooks (eslint/tsc/vitest) run in other packages. Added
  `install_repo_node_deps` (every tracked non-vendored `package.json`) before the
  pre-commit run so those hook binaries are on PATH.
- **Resolved by 3.8:** the aider loop is no longer backend-only. `detect_test_cmd`
  emits a per-module chain (e.g. `cd backend && go test ./... && cd ../frontend &&
  npm run test`), so a cross-stack repo now gets a frontend signal *inside* the
  loop, not just at the verify gate. A higher VERIFY_MAX_ROUNDS is still worth
  considering independently.

NOT DONE (deferred, needs a decision or is lower value):
- **4.2** no-reviewer policy — product decision (fail-fast probe vs. merge-on-green).
- **4.4** small items — pagination note, greedy `extract_json`, doc human-approve,
  scoping `ls-files` truncation, merge-conflict rebase retry.
- **Testing strategy** — bats harness + CI shellcheck not yet added.
- **Job-log fetch nuance (3.1):** annotations + a `curl -L` job-log tail are in;
  `-L` drops the auth header on the cross-host redirect so the token is not
  leaked. Providers other than GitHub Actions 404 and degrade to annotations.

### 1.5 Review convergence — the reviewer never approves  ⬅ from job 2f012766 (aider+Claude)

Once the coding model became strong enough (aider on Claude) to produce a
building, test-green PR, the failure moved entirely into the review loop: the
PR was correct but `oglimmer/review-action` requested changes every round and
never approved, burning all `REVIEW_MAX_ROUNDS`. Root cause was in the reviewer,
not here: it re-reviewed each push **statelessly**, surfacing a fresh (often
contradictory) nit per round — round 3 flagged "unbounded log loading", the fix
added a 500-cap, the final round flagged the 500-cap as "contradicting complete
details". No model can converge against that. Fixed in the *review-action* repo:
it now (a) returns an explicit `approve`/`request_changes` **verdict** with
severity discipline (design/completeness preferences are non-blocking), (b) is
fed the prior round's findings so it stops re-litigating resolved points, and
(c) submits a real `APPROVE`/`REQUEST_CHANGES` review. The worker already merges
on a formal `APPROVED` for the head sha, so that alone closes the loop. Worker
side (this repo): `verdict_from_marker` reads review-action's machine-readable
`<!-- review-verdict:… reviewed-sha:… -->` marker as the authoritative signal in
the no-formal-verdict branch, ahead of the DeepSeek prose judge (which is lossy
and biased to `needs_changes`). The prose judge remains the last-resort fallback
for reviewers that don't emit the marker.

---

## Phase 1 — review-loop convergence (turns "work done" into "job failed")

### 1.1 Stale formal `CHANGES_REQUESTED` blocks merge forever

- **Problem:** `REVIEW_STATE` is the *last* formal review across ALL commits
  (`jq '... | last | .state'`). The review action posts a formal review object
  only when it has inline findings; after a successful fix it merely edits its
  sticky comment. The old `CHANGES_REQUESTED` therefore stays "last" forever →
  `decision=fix` every round → all `REVIEW_MAX_ROUNDS` burned → job fails on an
  approved change. The prose judge is never consulted because it only runs in
  the `*)` case of the verdict switch.
- **Where:** `wait_for_review`, run_agent.sh:536-538; decision switch :649-660.
- **Fix:** Only honour a formal verdict that belongs to the current head
  commit; otherwise treat it as "no formal verdict" and fall through to the
  prose judge.
- **Hints:**
  - The reviews endpoint returns `commit_id` per review. `wait_for_review`
    already receives `head_sha` — add it to the jq filter:
    `--arg sha "$head_sha"` … `select(.commit_id == $sha)`.
  - Keep the safe direction: a fresh `CHANGES_REQUESTED` (matching head) still
    means fix; an *old* `APPROVED` must also not auto-merge — the sha filter
    handles both.
- **Verify:** Simulate round 2: PR with a `CHANGES_REQUESTED` review on commit
  A, head now at commit B with a positive sticky comment → decision must come
  from the prose judge, not the stale verdict.

### 1.2 The final fix round is pushed and never evaluated

- **Problem:** Loop order is wait → decide → fix → push → `round++` →
  loop-cond. With `REVIEW_MAX_ROUNDS=3` the 3rd fix is pushed, then
  `round=3 < 3` fails and the job exits `failed` without waiting for the
  review of that final push. The best-informed fix round is always wasted.
- **Where:** review loop, run_agent.sh:636-683.
- **Fix:** Every pushed fix must get one more wait+decide. Run wait/decide
  `REVIEW_MAX_ROUNDS + 1` times and only allow a fix in the first
  `REVIEW_MAX_ROUNDS` iterations:
  ```bash
  for attempt in $(seq 0 "$REVIEW_MAX_ROUNDS"); do
    wait_for_review "$(git rev-parse HEAD)" || fail ...
    decide ...
    [ "$decision" = merge ] && { merge; break; }
    [ "$attempt" -eq "$REVIEW_MAX_ROUNDS" ] && break   # no more fixes; leave PR open
    collect_findings; run_aider ...; push
  done
  ```
- **Verify:** With MAX=1: initial review → fix → push → the fix's review must
  still be awaited and can merge.

### 1.3 Stale sticky comment + `NO_CHECK_GRACE` race after a fix push

- **Problem:** "Reviewer responded" (`REVIEW_SEEN`) is satisfied by the mere
  *presence* of the sticky summary comment — which persists (edited in place)
  from the previous round. Freshness is delegated to "this commit's checks
  completed", but if GitHub Actions hasn't *registered* a check run within
  `NO_CHECK_GRACE` polls (3 × 20s = 60s; Actions queue delays exceed this
  routinely), `total==0` passes the grace and the worker acts on LAST round's
  review text: stale-positive → **merges an unreviewed commit**;
  stale-negative → re-fixes already-fixed findings.
- **Where:** `wait_for_review` run_agent.sh:543-556; also `fetch_review_summary`
  :506-511 and `collect_findings`/`gather_review_text`, which read the same
  possibly-stale comment.
- **Fix:** Gate on the sticky comment's `updated_at` being newer than the
  moment we pushed the commit under review.
  - Record `PUSH_TS=$(date -u +%FT%TZ)` immediately **before** each
    `git push` (initial and fix pushes).
  - In the `seen` query and in `fetch_review_summary`, filter:
    `select(.updated_at > $since)` — ISO-8601 UTC strings compare correctly as
    plain jq string comparisons.
  - Belt-and-braces (we own `oglimmer/review-action`): have the action embed
    `<!-- reviewed-sha:<sha> -->` in the sticky comment and match it against
    head; then the timestamp becomes a fallback.
- **Verify:** Push a fix while the previous round's comment exists; worker must
  keep polling until the comment's `updated_at` moves past the push, even when
  check-run registration is slow.

### 1.4 `gh_api` treats API errors as empty results (feeds 1.3)

- **Problem:** No HTTP status checking anywhere. A 403 rate-limit or 5xx during
  polling parses as "0 check runs / no comments", which the grace-period logic
  then trusts.
- **Where:** `gh_api`, run_agent.sh:86-100; all poll sites.
- **Fix:** Capture status (`curl -w '\n%{http_code}'` or `--fail-with-body`
  plus a retry wrapper). In `wait_for_review`, a failed fetch must mean "not
  ready, poll again" — never "no checks".
- **Hints:** Return non-zero from `gh_api` on status >= 400; at the poll sites
  `continue` the loop on failure. Add small retry (2-3 attempts, sleep 5) for
  idempotent GETs. Log the status + `x-ratelimit-remaining` header when it
  happens.
- **Verify:** Point `API` at a URL returning 500 → `wait_for_review` keeps
  polling instead of declaring `checks_done` via the grace path.

---

## Phase 2 — preserve completed work (don't discard the model's output)

### 2.1 Aider timeout (rc=124) discards committed work  ⬅ CONFIRMED IN PROD

- **CONFIRMED by job `226cfe38` (repo `oglimmer/irl-planner-pro`, 2026-07-08):**
  the FIRST aider run (:311) hit the 3600s timeout, was killed (rc 124), and
  `|| fail "aider run failed"` ended the job with the branch never pushed and no
  PR opened. Final line:
  `{"status":"failed","reason":"aider run failed","branch":"agent/…admins-…"}`.
  This is currently the single most common way a real run dies. It was made
  worse by 3.5 below (auto-test was disabled, so aider had no convergence signal
  and burned the whole hour). Fix 2.1 first.
- **Problem:** `run_aider` returning 124 hits `|| fail` at every call site.
  Architect mode commits incrementally, so a round that timed out during a
  final reflection may already contain the whole feature — thrown away, no
  push, no PR.
- **Where:** `run_aider` :279-299 and call sites :311, :329, :381, :455, :677.
- **Fix:** Treat 124 as "round ended, keep what it committed":
  - Main run (:311): if rc==124 **and** `git rev-list --count
    origin/$BASE_BRANCH..HEAD` > 0 → warn + continue to the gates (they
    re-check everything anyway); if no commits → fail as today.
  - Corrective rounds (test-add, self-review, verify, review-fix): on 124 just
    continue — the surrounding gate re-evaluates and decides.
  - Keep failing on *other* non-zero rcs (real aider crashes).
- **Hints:** Give `run_aider` a distinguishable return; simplest is to have
  call sites capture `rc=$?` and branch on `124` explicitly instead of `||`.
- **Verify:** Set `AIDER_TIMEOUT=30` against a real task; job should still
  proceed to self-review/verify with whatever got committed.

### 2.2 Verify-gate failure exits with nothing pushed

- **Problem:** `fail "local verification ... no PR opened"` (:464-466) discards
  the branch entirely. A human could often finish a 90%-done change.
- **Where:** `verify_gate` failure path, run_agent.sh:464-466.
- **Fix:** Before `fail`, push the branch anyway (no PR, or a **draft** PR:
  `{"draft":true}` on the pulls POST) and include the branch name + the tail of
  `/work/verify.log` in the emitted failure JSON so the UI can show "needs a
  human, branch pushed: X".
- **Hints:** Extend the `fail` payload: `{status:"failed", reason:..., branch:...,
  pushed:true}`. Backend already greps `CODING_AGENT_RESULT:` — check
  `backend/internal` result parsing tolerates the extra field (it should; it's
  JSON).
- **Verify:** Force a failing `AGENT_VERIFY_CMD` (`false`); branch must exist on
  origin after the failed job.

---

## Phase 3 — feedback quality (make fix rounds actually informed)

### 3.1 CI-failure findings carry no logs

- **Problem:** `collect_findings` gives aider `output.summary // output.title
  // "failed"` per failed check — for most Actions runs that's empty, so aider
  fixes CI blind.
- **Where:** run_agent.sh:586-588.
- **Fix:** Enrich each failed check with real detail, in order of cheapness:
  1. Check-run annotations: `GET ${API}/check-runs/{check_run_id}/annotations`
     → `"\(.path):\(.start_line): \(.message)"`.
  2. Actions job logs: check run `.id` maps to an Actions job —
     `GET ${API}/actions/jobs/{job_id}/logs` (302 redirect → `curl -L`,
     plain text). Take `tail -c 8000` per failed job.
- **Hints:** The check-run id from `check-runs` IS the Actions job id for
  workflow-created checks. Guard with `head -c` limits so findings.txt stays
  within the model context. Keep the existing summary as fallback.
- **Verify:** Break a test on a repo with CI; findings.txt must contain the
  actual assertion failure text.

### 3.2 Python repos silently lose the auto-test loop

- **Problem:** `prepare_test_cmd` installs npm deps only. A pytest command on a
  repo with uninstalled requirements fails at baseline → auto-test disabled →
  aider codes with zero compiler/test feedback ("the main quality lever" per
  the Dockerfile comment).
- **Where:** `prepare_test_cmd`, run_agent.sh:208-230.
- **Fix:** When the test cmd invokes `python|python3|pytest`, create a target
  venv and install deps before the baseline run:
  ```bash
  python -m venv /work/target-venv
  export PATH="/work/target-venv/bin:$PATH"   # inherited by aider's test subprocess
  pip install -q pytest
  [ -f "$dir/requirements.txt" ] && pip install -q -r "$dir/requirements.txt"
  [ -f "$dir/pyproject.toml" ]  && pip install -q -e "$dir" || true
  ```
- **Hints:** `export PATH` is enough — aider runs `--test-cmd` via a subprocess
  that inherits env, and the verify gate's `bash -c` does too. Log install
  failures like the npm path does (disable auto-test, don't fail the job).
- **Verify:** Run against a Python repo with requirements.txt; baseline must go
  green and `--auto-test` must stay enabled.

### 3.3 Self-review judges a truncated / noise-dominated diff

- **Problem:** `git diff ... | head -c 60000` truncates the diff alphabetically
  by path; a regenerated lockfile early in the alphabet crowds out the real
  change → false "not implemented" → burns both corrective rounds on a correct
  diff.
- **Where:** `self_review`, run_agent.sh:347.
- **Fix:**
  - Exclude generated files from the judged diff:
    `git diff "origin/$BASE_BRANCH..HEAD" -- . ':(exclude)package-lock.json'
    ':(exclude)yarn.lock' ':(exclude)pnpm-lock.yaml' ':(exclude)go.sum'
    ':(exclude)*.lock'`.
  - Prepend `git diff --stat` output so the judge always sees the full shape
    even when the body is truncated.
  - Optionally truncate per-file (loop `git diff -- "$f" | head -c 4000`)
    instead of one global cut.
- **Verify:** Craft a branch with a huge lockfile change + a small real change;
  judge must still see the real change.

### 3.4 Review-fix rounds drop the scope files

- **Problem:** The review-fix `run_aider` call passes only `branch_files`,
  unlike the self-review and verify rounds which also pass `SCOPE_FILES`. A fix
  that must touch a not-yet-touched file (route registration, config) relies on
  aider asking to add it.
- **Where:** run_agent.sh:676-677 vs :381, :455.
- **Fix:** Append `${SCOPE_FILES[@]+"${SCOPE_FILES[@]}"}` there too (aider
  dedupes repeated files).
- **Verify:** Trivial; eyeball the aider "files in chat" log line in round 2.

### 3.5 `prepare_test_cmd` mishandles cross-stack / multi-`cd` commands  ⬅ NEW (from job 226cfe38)

> **Superseded by 3.8.** Multi-`cd` dir tracking now lives in `prepare_cmd_deps`
> (and 3.8b fixed its last-segment drop). The command is no longer model-produced;
> `detect_test_cmd` emits the cross-stack chain directly.


- **Problem:** The scope model routinely returns a compound command that spans
  two stacks, e.g. `cd backend && go test ./... && cd ../frontend && npm run
  test:unit`. `prepare_test_cmd` only understands a SINGLE leading `cd`: its dir
  detection (`case "$SCOPE_TEST_CMD" in "cd "*)`) extracts `backend` and looks
  for `backend/package.json` — which doesn't exist — so it installs no npm deps
  at all, then the `npm run test:unit` segment (in `../frontend`, never
  installed) fails at baseline and **auto-test is disabled**. In job 226cfe38
  the baseline died on `npm error Missing script: "test:unit"` and aider then
  ran the whole 3600s with no test loop (→ 2.1 timeout).
- **Where:** `prepare_test_cmd`, run_agent.sh:208-230.
- **Fix:** Handle each `cd X && …` segment independently:
  - Split the command on `&&`, track the "current dir" as `cd` segments change,
    and for every segment invoking `npm|npx|node`, run `npm ci/install` in the
    dir in effect at that point (not just the first `cd`).
  - Same idea generalises the Python venv work in 3.2 — install deps per stack
    that the command actually touches.
- **Verify:** Feed the exact 226cfe38 command with a real `test:unit` script;
  both `backend` and `frontend` deps must be installed and baseline go green.

### 3.6 Scope model hallucinates a non-existent test target; no fallback lever  ⬅ NEW (from job 226cfe38)

> **Superseded by 3.8.** The model no longer proposes an executable test command,
> so a hallucinated target can't occur. The `check_npm_script` guard and the
> compile/vet fallback below are retained as defence in depth.


- **Problem:** Two compounding gaps exposed by 226cfe38:
  1. The model invented an npm script (`test:unit`) that isn't in the frontend
     `package.json`. `validate_test_cmd` only checks the *tool* (`npm`) exists,
     not that `npm run <script>` is defined — so a hallucinated target sails
     through validation and only the baseline run catches it (by failing).
  2. Once baseline fails, auto-test is disabled and **nothing replaces the
     quality lever** — aider codes blind. This is what let the run drift to the
     full-hour timeout.
- **Where:** `validate_test_cmd` :161-178; `prepare_test_cmd` :208-230; the
  no-auto-test path generally.
- **Fix:**
  - Cheap pre-check for `npm run <script>`: verify `<script>` is a key under
    `.scripts` in the relevant `package.json` (`jq -e --arg s "$script"
    '.scripts[$s]' package.json`); drop just that segment (or re-scope) if not.
  - When auto-test ends up disabled, fall back to a **zero-config compile/vet
    gate** that needs no test script: `go build ./... && go vet ./...` for Go,
    `tsc --noEmit` / `npm run build` for a JS/TS stack detected from
    `package.json`. It won't assert behaviour but it keeps a syntax/type signal
    in the aider loop instead of nothing.
  - Consider a shorter first-round `AIDER_TIMEOUT` (e.g. 1200s) combined with
    2.1's preserve-work behaviour: a killed round then still yields a reviewable
    PR rather than an hour of silence followed by a discard.
- **Verify:** Give a repo whose scoped test cmd names a missing script; the run
  must either re-scope or proceed with the compile/vet fallback, and never sit
  for an hour with no signal.

---

## Phase 4 — hardening / policy

### 4.1 `.aider*` gitignore commit defeats the "made no commits" guard

- **Problem:** With `--yes-always`, aider commits ".aider* added to .gitignore"
  housekeeping, so a run with zero feature work still shows 1 commit and passes
  the `:333` guard, wasting downstream gate rounds.
- **Fix:** Add `--no-gitignore` to the aider invocation (run_agent.sh:279-292).
  Additionally make the guard semantic:
  `git diff --name-only origin/$BASE..HEAD | grep -vq '^\.gitignore$' || fail`.
- **Verify:** Prompt aider with an impossible/no-op task; job must fail with
  "no commits", not proceed to self-review.

### 4.2 Repos without the review action can never succeed — decide the policy

- **Problem:** `wait_for_review` requires the sticky comment (or a failed
  check), so a repo with green checks and no reviewer waits the full 30-min
  `REVIEW_TIMEOUT` and fails. Every run. Silently.
- **Fix options** (pick one, make it explicit):
  a) **Fail fast with a clear reason** (safest): after clone, probe
     `.github/workflows/*` for the review action (`grep -rl review-action
     .github/workflows` or via the contents API); if absent, `fail "repo has
     no review action configured"` *before* spending aider tokens.
  b) **Merge-on-green fallback**: new env `AGENT_MERGE_ON_GREEN` (default
     false); when no reviewer is detected and checks are green, run the prose
     judge over "no review" or merge directly.
- **Hints:** (a) is a 10-line change and converts a 30-minute token-burning
  hang into an instant, actionable error. (b) is a product decision — check
  with the repo-onboarding flow in `backend/internal/server` first.
- **Verify:** Point a job at a repo without the action; observe fast failure
  (or merge-on-green if (b)).

### 4.3 `AGENT_VERIFY_CMD` is never baseline-checked

- **Problem:** `SCOPE_TEST_CMD` must pass on the untouched branch or it's
  disabled; the repo-configured `VERIFY_CMD` gets no such check. A broken repo
  config (typo, missing tool) burns the full aider run + all
  `VERIFY_MAX_ROUNDS` chasing phantom failures, then fails the job.
- **Where:** run_agent.sh:396 (`EFFECTIVE_VERIFY`), `prepare_test_cmd` :208.
- **Fix:** Run `EFFECTIVE_VERIFY` once on the clean baseline (right after
  `prepare_test_cmd`). If red at baseline: log loudly, emit it in the result
  JSON (`verify_baseline:"red"`), and either fail fast ("repo verify command
  broken — fix repo settings") or degrade to `SCOPE_TEST_CMD`. Failing fast is
  probably right: the repo owner set it, they should know it's broken.
- **Verify:** Configure `VERIFY_CMD="go test ./nonexistent"` on a repo; job
  should fail before any aider round.

### 4.4 Smaller items (batch into any of the above PRs)

- **Pagination:** all list endpoints fetch page 1 only (`per_page=100`). Fine
  today; add a comment stating the assumption so nobody trusts it for >100
  comments.
- **`extract_json`** (:117-119): greedy `grep -o '{.*}'` grabs from first `{`
  to *last* `}` — trailing prose with a `}` corrupts the JSON. Consider
  `jq -R 'fromjson?'` line-wise or a lazy match; current judges usually emit
  clean JSON, so low priority.
- **Human `APPROVED` merges instantly** (:536-538 selects any non-bot review).
  Probably desired — document it in the header comment.
- **`git ls-files | head -3000`** (:182): alphabetical truncation can hide
  whole directories from scoping on big repos. If it ever bites, prefer a
  depth-limited directory summary + targeted `ls-files` per candidate dir.
- **Merge conflicts with a moved base branch** are reported as "auto-merge
  failed" with no retry; a `git fetch origin && git rebase origin/$BASE` +
  re-push attempt before giving up would rescue the common case.

---

## Testing strategy (applies to all phases)

- The GitHub-interaction logic (`wait_for_review`, verdict selection, decision
  switch) is now complex enough to unit test. Extract the jq filters and the
  decision function into small pure helpers and test with canned API JSON via
  [bats](https://github.com/bats-core/bats-core) under `worker/tests/`.
  Fixtures to cover: stale CHANGES_REQUESTED (1.1), stale sticky comment
  (1.3), 0-checks grace, API 500 (1.4), final-round merge (1.2).
- Run `shellcheck worker/run_agent.sh` in CI (it's already mostly clean).
- Keep an end-to-end smoke path: a sandbox repo + a trivial feature request,
  run via `oglimmer.sh`, asserting the emitted `CODING_AGENT_RESULT` line.
