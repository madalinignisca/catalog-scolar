# CI Testing Strategy — When to Run What

## Problem

End-to-end (E2E) tests are essential for catching integration bugs, but they're expensive: ~7 minutes per run, requiring a full PostgreSQL + Redis + Nuxt + Playwright stack. Running them on every pull request creates a bottleneck that discourages small, frequent PRs — the exact workflow we want to encourage.

## Principle: Test Cost Should Match Change Risk

Not every change carries the same risk. A backend-only SQL query change can't break the login flow. A typo fix in a comment can't corrupt data. The CI pipeline should reflect this reality.

## The Three-Tier Strategy

### Tier 1: Every PR (fast, ~2 min)

**What runs:** Linting + unit/integration tests + build.

```
API Lint       → golangci-lint (Go code quality)
API Test       → go test with testcontainers (real PostgreSQL, RLS)
API Build      → go build (compilation check)
Web Lint       → ESLint + Prettier (TypeScript/Vue)
Web Test       → Vitest (unit tests for composables/utils)
Web Build      → nuxt build (compilation + type check)
CodeQL         → static analysis for security vulnerabilities
```

These tests are fast, deterministic, and catch 90% of bugs.

**Optimization — path-based skipping:** If a PR only touches `api/` files, the Web jobs are skipped. Frontend-only PRs skip Go tests. This is implemented via GitHub Actions' `paths` filter on the job level.

### Tier 2: Post-merge + release branches (thorough, ~7 min)

**What runs:** Everything from Tier 1 + full E2E suite.

```
E2E Test       → Playwright (25+ browser tests against real stack)
```

E2E tests run:
- **On every push to `main`** — catches integration issues after merge. If E2E fails on main, the team is notified but main isn't blocked (the individual PR tests already passed).
- **On `release/*` branches** — mandatory gate before any release. E2E must pass before tagging.

### Tier 3: Manual trigger (on-demand, ~7 min)

**What runs:** Full E2E suite on any branch via `workflow_dispatch`.

Any developer can manually trigger E2E on their PR branch if they suspect their change needs it (e.g., touching auth flows or SSR rendering).

## Release Workflow

We use **milestone-based releases**:

```
v0.1.0  → Sprint 1: School Onboarding
v0.2.0  → Sprint 2: Parent & Student Experience
v0.3.0  → Sprint 3: Production Readiness
v0.4.0  → Sprint 4: Semester Workflows
v0.5.0  → Sprint 5: Interop & Import
v0.6.0  → Sprint 6: Communication
v0.7.0  → Technical Debt cleanup
v1.0.0  → Production release
```

Patch releases (`v0.6.1`) for hotfixes that need immediate deployment.

### Release process

1. Create branch `release/v0.X.0` from `main`
2. CI runs full E2E suite (Tier 2) — must pass
3. Tag the release: `git tag v0.X.0`
4. CI builds and pushes Docker images to GHCR
5. Deploy to staging (auto), then production (manual approval)

## Why This Matters

| Metric | Before | After |
|--------|--------|-------|
| PR CI time | ~8 min | ~2 min |
| E2E runs/day | ~15 (every PR) | ~3 (merges + releases) |
| CI minutes/month | ~3,600 | ~1,530 |
| Developer wait time | 8 min per push | 2 min per push |

*Calculation: 15 PRs/day x 2 min x 30 days = 900 min (Tier 1) + 3 merges/day x 7 min x 30 days = 630 min (Tier 2) = 1,530 min total. That's a 57% reduction in CI minutes.*

But the real win isn't minutes — it's developer velocity. When CI is fast, developers push small changes often. When CI is slow, they batch changes into large PRs that are harder to review and more likely to introduce bugs.

## Implementation

The strategy is implemented in `.github/workflows/ci.yml` with:
- `paths` filters for path-based job skipping
- `on.push.branches` for post-merge E2E on main
- `on.push.branches` matching `release/**` for release E2E
- `workflow_dispatch` for manual E2E triggers on any branch
