# CatalogRO

Digital school catalog platform for Romanian primary, middle, and high schools.

**Product spec:** See [SPECS.md](SPECS.md) for the full technical plan (Romanian) — schema design, API contracts, auth flows, interop architecture, and implementation timeline.

## Stack

- **API:** Go 1.24, chi router, sqlc, goose migrations, River jobs
- **DB:** PostgreSQL 17 with Row-Level Security (multi-tenant via school_id)
- **Web:** Nuxt 3 (SSR), TypeScript strict, Tailwind CSS, Dexie.js (IndexedDB)
- **Infra:** K3S on Hetzner Cloud (EU), Helm, Traefik, cert-manager

## Monorepo layout

- `api/` — Go backend. Run: `cd api && go run ./cmd/server`
- `web/` — Nuxt 3 frontend. Run: `cd web && npm run dev`
- `helm/` — Kubernetes charts

## Commands (Makefile)

- `make dev` — start docker-compose + API + web dev servers
- `make test` — run Go + Nuxt tests
- `make migrate` — run goose migrations up
- `make migrate-down` — rollback last migration
- `make sqlc` — regenerate sqlc output
- `make seed` — load seed data (2 test schools)
- `make lint` — golangci-lint + eslint

## Database conventions

- All tables have `school_id UUID NOT NULL` for RLS (except `schools`, `districts`)
- RLS policies use `current_setting('app.current_school_id')::uuid`
- Migrations in `api/db/migrations/` using goose (SQL only, no Go migrations)
- Queries in `api/db/queries/` — run `make sqlc` after changes
- Timestamps: `created_at TIMESTAMPTZ DEFAULT now()`, `updated_at TIMESTAMPTZ`
- Soft delete: `deleted_at TIMESTAMPTZ` where needed (never hard delete student data)
- UUIDs for all PKs (v7 for time-ordered, v4 for random)

## Auth model

- JWT access tokens (15min) + refresh tokens (7d, Redis-backed)
- 2FA (TOTP) mandatory for roles: teacher, admin, secretary
- Tenant resolved from JWT claim `school_id`, set via RLS middleware
- NO self-registration. Secretary/admin provisions accounts with known data
- Users receive activation link (email/SMS), set password + 2FA on first access
- Parents accept GDPR consent at activation; child account becomes visible after
- Bulk import from SIIIR creates accounts in batch, generates activation links

## Evaluation rules

- Primary (classes P-IV): qualifiers FB/B/S/I, descriptive evaluations, no numeric average
- Middle school (V-VIII): grades 1-10, semester thesis weighted, arithmetic average
- High school (IX-XII): grades 1-10, thesis, BAC prep. Same average rules as middle
- Rules are configurable per school via `evaluation_configs` table, NOT hardcoded

## Offline sync

- IndexedDB (Dexie.js) stores local cache of catalog data per class
- Mutations go to sync queue (IndexedDB table `_sync_queue`)
- Queue flushes on reconnect, sequential, with exponential backoff
- Server resolves conflicts: last-write-wins based on `client_timestamp`
- Both versions preserved in `sync_conflicts` table for audit

## Interoperability & standards

- **OneRoster 1.2** is the data exchange standard. Entities map: School→Org, Class→Class, Student→User, Grade→Result
- **source_mappings** table links internal entity IDs to external IDs (SIIIR, OneRoster sourcedId, etc.)
- **SIIIR import**: Parser-based (CSV). Column definitions versioned in `interop/siiir/columns.go`. Auto-detects format, encoding (UTF-8 / Windows-1250), delimiters
- **SIIIR export**: Reverse mapping for ISJ reporting
- **EHEIF alignment**: Student Record Package — portable JSON with full academic history
- **EIF compliance**: API-first, OpenAPI 3.1 spec at `/.well-known/openapi.json`
- **Adapter pattern**: `interop/registry.go` selects parser/exporter by source format. New formats = new adapter, zero changes to core schema

## Code style

- Go: golangci-lint, gofmt, error wrapping with fmt.Errorf("op: %w", err)
- TS: ESLint flat config v9 + typescript-eslint + eslint-plugin-vue, Prettier
- Commits: conventional commits (feat/fix/chore/docs)
- No `any` in TypeScript. Strict mode enabled.

## Testing

- Go: table-driven tests, testcontainers-go for PG integration tests
- Nuxt: Vitest for composables/utils, Playwright for E2E
- CI runs both on every PR

## Important domain terms (Romanian)

- notă = grade, absență = absence, medie = average, teză = thesis/exam
- calificativ = qualifier (FB/B/S/I), diriginte = homeroom teacher
- corigent = student who must retake exam, repetent = student repeating year
- ISJ = Inspectoratul Școlar Județean (county school inspectorate)
- SIIIR = Sistemul Informatic Integrat al Învățământului din România
- ROFUIP = Regulamentul de Organizare și Funcționare a Unităților de Învățământ Preuniversitar

## Environment variables (api)

```
DATABASE_URL=postgres://catalogro:catalogro@localhost:5432/catalogro?sslmode=disable
REDIS_URL=redis://localhost:6379/0
JWT_SECRET=<random-32-bytes-hex>
TOTP_ENCRYPTION_KEY=<random-32-bytes-hex>
SMTP_HOST=smtp.scoala-rebreanu.ro
SMTP_PORT=587
SMTP_USERNAME=catalog@scoala-rebreanu.ro
SMTP_PASSWORD=<secret>
SMTP_FROM=catalog@scoala-rebreanu.ro
SMTP_TLS=starttls
MINIO_ENDPOINT=localhost:9000
MINIO_ACCESS_KEY=minioadmin
MINIO_SECRET_KEY=minioadmin
VAPID_PUBLIC_KEY=<base64url-encoded-public-key>
VAPID_PRIVATE_KEY=<base64url-encoded-private-key>
VAPID_CONTACT=mailto:admin@catalogro.ro
APP_BASE_URL=http://localhost:3000
PORT=8080
ENV=development
```

## Container registry

- Images stored on **GHCR** (GitHub Container Registry): `ghcr.io/vlahsh/catalogro-api`, `ghcr.io/vlahsh/catalogro-web`
- CI pushes on every merge to `main` with two tags: git SHA short (e.g. `a1b2c3d`) and `latest`
- K3S clusters pull via `imagePullSecrets` named `ghcr-credentials`
- Create pull secret: `kubectl create secret docker-registry ghcr-credentials --docker-server=ghcr.io --docker-username=vlahsh --docker-password=<PAT> -n <namespace>`

## Deploy pipeline

- **CI** (`ci.yml`): lint → test → build → push to GHCR (on main only)
- **Staging** (`deploy-staging.yml`): auto-triggers after CI success, `helm upgrade` with SHA tag
- **Production** (`deploy-prod.yml`): manual trigger via `workflow_dispatch`, requires `image_tag` input
- GitHub environments: `staging` (auto), `production` (manual approval recommended)
- Secrets needed: `KUBECONFIG_STAGING`, `KUBECONFIG_PROD` (base64-encoded kubeconfig)
