# coding-agent

Self-service platform where authenticated users request features against configured GitHub
repositories and an autonomous coding agent implements them end-to-end — **with tests** — opens a
pull request, waits for the repository's GitHub Action review, fixes the findings, and auto-merges.

## How it works

```
 user ──▶ Vue SPA ──▶ Go API ──▶ DeepSeek harmful-content gate
                          │            │ (harmful → rejected, no job)
                          │            ▼
                          └──▶ Kubernetes Job (worker)
                                   1. scope: DeepSeek picks the relevant files + test
                                      command from the repo listing
                                   2. aider (architect mode: DeepSeek planner + editor)
                                      implements the feature (+ tests), auto-running the
                                      repo's test suite after each edit
                                   3. pre-PR self-review: a judge model checks the diff
                                      actually implements the request; corrective rounds
                                   4. push branch, open PR
                                   5. wait for the repo's GitHub Action review
                                   6. fix findings with aider, re-review  (up to N rounds)
                                   7. squash-merge once checks are green and the review
                                      (formal verdict or LLM-judged prose) approves
                          ◀── backend watcher polls the Job, records status + PR URL
```

- **SSO/OIDC login.** Any OIDC provider (env-configured); a dev password stub for local work.
  **The first user to sign in becomes an admin.** Admins grant/revoke admin on other users.
- **Admins configure the repository list** (owner/name/base-branch, stored in Postgres). Every
  logged-in user can pick a repo and submit a feature description.
- **Harmful requests are blocked** by a DeepSeek classifier before any compute is spent.
- The **prompt always requires tests**, even when the user doesn't mention them.
- The **review→fix→merge loop** is what distinguishes this from a fire-and-forget PR bot.

This reuses the proven `/vibecode` mechanism from `vibe-coding-discord-bot` (k8s Job + aider +
DeepSeek), ported to Go and extended with the review loop.

## Layout

| Path | What |
|------|------|
| `backend/` | Go API (chi, pgx/Postgres, go-oidc, JWT), spawns/watches worker Jobs |
| `frontend/` | Vue 3 + Vite + TS SPA (Pinia, vue-router) |
| `worker/` | Docker image + `run_agent.sh`: clone → aider → PR → review-fix-merge |
| `helm/coding-agent/` | Helm chart (backend, frontend, bundled Postgres, ingress, RBAC) |
| `.github/workflows/` | CI gate, image build, release, cleanup |

## Local development

```sh
cp .env.example .env
./oglimmer.sh start            # Postgres + backend on :8080
cd frontend && npm install && npm run dev   # SPA on :5173 (proxies /api)
```

Sign in with the dev password (`AUTH_MODE=password`, `DEV_PASSWORD=dev`). The first sign-in is admin.
Add a repository, then submit a feature request. Job spawning requires a Kubernetes cluster; without
one the API still runs and job creation returns 503 (the harmful-content gate is still exercisable).

## Tests

```sh
./oglimmer.sh test    # backend: gofmt/vet/go test -race ; frontend: typecheck/lint/vitest ; worker: shellcheck
```

## Deploy

Build and push images (`./oglimmer.sh build -a` or tag `v*` for the release workflow), then install
the chart. Provide a Secret (`<release>-coding-agent-secret`) with `JWT_SECRET`, `DEEPSEEK_API_KEY`,
`WORKER_GITHUB_TOKEN`, `POSTGRES_PASSWORD` (bundled DB) or `DATABASE_URL` (external), and
`OIDC_CLIENT_SECRET` (OIDC mode).

```sh
helm install coding-agent helm/coding-agent \
  --set publicBaseURL=https://coding-agent.example.com \
  --set auth.oidc.issuer=https://id.example.com/... \
  --set auth.oidc.clientId=coding-agent
```

## Assumptions

- Every configured repository has a GitHub Action that reviews PRs (a PR review of any state —
  `APPROVED`, `CHANGES_REQUESTED`, or `COMMENTED` — and/or status checks). The worker waits for CI
  to finish and the reviewer to respond, then: **fixes** when a check fails or the review is
  `CHANGES_REQUESTED` (up to `REVIEW_MAX_ROUNDS`), and **squash-merges** once checks are green and no
  changes were requested. A `COMMENTED` review with passing checks does not block the merge.
- Coding agent = aider + `deepseek/deepseek-v4-pro` (swappable via `WORKER_MODEL`).

## License

[MIT](LICENSE) © Oli Zimpasser
