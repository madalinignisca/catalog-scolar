# CatalogRO — Plan Tehnic de Pornire

> **Versiune:** 0.3-draft · **Data:** Martie 2026
> **Stack:** Go 1.24 · PostgreSQL 17 (RLS) · Nuxt 3 PWA · K3S Hetzner
> **Decizii luate:** Monorepo · IndexedDB + sync queue · Multi-tenant SaaS · PWA-only
> **Auth model:** Provizionare de secretariat + activare cont (fără self-registration)
> **Interop:** OneRoster 1.2 ca standard de date · SIIIR parser CSV · EHEIF portabilitate elev · EIF API-first

---

## 1. Structura Monorepo

```
catalogro/
├── .github/
│   └── workflows/
│       ├── ci.yml                  # lint + test + build (Go + Nuxt)
│       ├── deploy-staging.yml      # auto-deploy pe push la main
│       └── deploy-prod.yml         # manual trigger / tag-based
├── CLAUDE.md                       # instrucțiuni Claude Code (vezi §1.1)
├── README.md
├── Makefile                        # comenzi unificate (make dev, make test, make migrate)
├── docker-compose.yml              # dev local: postgres, redis, mailpit
├── docker-compose.test.yml         # test environment cu PG izolat
│
├── api/                            # ── Go backend ──
│   ├── cmd/
│   │   └── server/
│   │       └── main.go             # entry point, wire dependencies
│   ├── internal/
│   │   ├── config/
│   │   │   └── config.go           # env-based config (12-factor)
│   │   ├── auth/
│   │   │   ├── middleware.go        # JWT validation, RLS context injection
│   │   │   ├── totp.go             # TOTP 2FA enrollment + verification
│   │   │   ├── session.go          # session management (Redis-backed)
│   │   │   └── activation.go       # account activation (token generation + validation)
│   │   ├── tenant/
│   │   │   ├── middleware.go        # extract school_id, SET app.current_school_id
│   │   │   └── resolver.go         # resolve tenant from JWT claims / subdomain
│   │   ├── catalog/
│   │   │   ├── handler.go          # HTTP handlers (note, absențe, medii)
│   │   │   ├── service.go          # business logic, validări reguli evaluare
│   │   │   ├── rules.go            # motor reguli: calificative vs note vs teze
│   │   │   └── sync.go             # procesare sync queue de la client
│   │   ├── school/
│   │   │   ├── handler.go          # CRUD școli, clase, materii, ani școlari
│   │   │   └── service.go
│   │   ├── user/
│   │   │   ├── handler.go          # provizionare, import bulk, activare, profil, GDPR
│   │   │   └── service.go
│   │   ├── messaging/
│   │   │   ├── handler.go          # mesaje 1:1 și grup
│   │   │   └── service.go
│   │   ├── notification/
│   │   │   ├── push.go             # Web Push API (VAPID)
│   │   │   ├── email.go            # Mailgun transactional
│   │   │   └── dispatcher.go       # fan-out: push + email + in-app
│   │   ├── report/
│   │   │   ├── handler.go          # generare rapoarte async
│   │   │   ├── catalog_pdf.go      # catalog tipărit format oficial
│   │   │   └── isj_export.go       # export format ISJ
│   │   ├── interop/                # ── Interoperabilitate & standarde ──
│   │   │   ├── oneroster/
│   │   │   │   ├── models.go       # structuri OneRoster 1.2 (Org, User, Class, Enrollment, Result, LineItem)
│   │   │   │   ├── export.go       # export date interne → OneRoster CSV/JSON
│   │   │   │   ├── import.go       # import OneRoster CSV/JSON → model intern
│   │   │   │   └── handler.go      # endpoints OneRoster-compatibile (Rostering + Gradebook)
│   │   │   ├── siiir/
│   │   │   │   ├── parser.go       # parser exporturi SIIIR (CSV cu coloane specifice: CNP, clasă, formă înv.)
│   │   │   │   ├── mapper.go       # mapping SIIIR → model intern (strat de abstractizare)
│   │   │   │   ├── exporter.go     # export model intern → format SIIIR raportare
│   │   │   │   └── columns.go      # definiții coloane SIIIR (versionabile, configurabile)
│   │   │   ├── portability/
│   │   │   │   ├── student_record.go  # pachet portabil elev (EHEIF-aligned: note, parcurs, evaluări)
│   │   │   │   └── handler.go         # export/import dosarul digital al elevului
│   │   │   └── registry.go         # registru de adaptoare: adaugă/selectează formatul de import/export
│   │   └── platform/
│   │       ├── database.go         # PG pool, RLS setup, migrations runner
│   │       ├── redis.go            # Redis client (cache + sessions + queues)
│   │       ├── storage.go          # MinIO/S3 client
│   │       └── jobs.go             # River job definitions
│   ├── db/
│   │   ├── migrations/             # goose migrations (SQL)
│   │   │   ├── 001_baseline.sql
│   │   │   ├── 002_catalog_core.sql
│   │   │   └── ...
│   │   ├── queries/                # sqlc query files
│   │   │   ├── schools.sql
│   │   │   ├── users.sql
│   │   │   ├── grades.sql
│   │   │   ├── absences.sql
│   │   │   ├── classes.sql
│   │   │   └── sync.sql
│   │   ├── sqlc.yaml
│   │   └── generated/              # sqlc output (DO NOT EDIT)
│   ├── go.mod
│   ├── go.sum
│   └── Dockerfile                  # multi-stage: build + distroless runtime
│
├── web/                            # ── Nuxt 3 frontend ──
│   ├── nuxt.config.ts
│   ├── app.vue
│   ├── pages/
│   │   ├── login.vue
│   │   ├── activate/
│   │   │   └── [token].vue         # activare cont (setare parolă + 2FA + GDPR)
│   │   ├── index.vue               # dashboard per rol
│   │   ├── catalog/
│   │   │   ├── [classId].vue       # vizualizare catalog clasă
│   │   │   └── [classId]/
│   │   │       └── [subjectId].vue # editare note per materie
│   │   ├── absences/
│   │   │   └── [classId].vue
│   │   ├── schedule/
│   │   │   └── index.vue
│   │   ├── messages/
│   │   │   └── index.vue
│   │   ├── reports/
│   │   │   └── index.vue
│   │   └── admin/
│   │       ├── school.vue          # configurare școală
│   │       ├── users.vue           # gestiune utilizatori + provizionare conturi
│   │       └── classes.vue         # clase, materii, încadrare
│   ├── components/
│   │   ├── catalog/
│   │   │   ├── GradeGrid.vue       # grid note editabil (componenta principală)
│   │   │   ├── GradeInput.vue      # input notă cu validare per nivel
│   │   │   ├── AbsenceToggle.vue   # toggle prezent/absent per oră
│   │   │   └── QualifierPicker.vue # picker FB/B/S/I pentru primar
│   │   ├── sync/
│   │   │   ├── SyncStatus.vue      # indicator sync (online/offline/syncing)
│   │   │   └── ConflictResolver.vue
│   │   └── ui/                     # componente generice
│   ├── composables/
│   │   ├── useAuth.ts              # auth state, JWT refresh, 2FA
│   │   ├── useTenant.ts            # school context
│   │   ├── useOfflineSync.ts       # CORE: sync queue + IndexedDB
│   │   ├── useCatalog.ts           # CRUD note/absențe (online + offline)
│   │   └── useNotifications.ts     # Web Push subscription
│   ├── lib/
│   │   ├── db.ts                   # IndexedDB schema (Dexie.js)
│   │   ├── sync-queue.ts           # mutation queue + retry logic
│   │   ├── sync-engine.ts          # orchestrator: detect online, flush queue
│   │   └── api.ts                  # fetch wrapper cu interceptori auth/tenant
│   ├── public/
│   │   └── sw.js                   # service worker (generat de @vite-pwa/nuxt)
│   ├── package.json
│   ├── tsconfig.json
│   ├── eslint.config.mjs           # flat config v9+ cu typescript-eslint + vue
│   ├── .prettierrc
│   └── Dockerfile                  # multi-stage: build Nuxt + serve static/SSR
│
├── helm/                           # ── Kubernetes deployment ──
│   └── catalogro/
│       ├── Chart.yaml
│       ├── values.yaml
│       ├── values-staging.yaml
│       ├── values-prod.yaml
│       └── templates/
│           ├── api-deployment.yaml
│           ├── web-deployment.yaml
│           ├── ingress.yaml        # Traefik IngressRoute
│           ├── secrets.yaml        # sealed secrets
│           └── cronjobs.yaml       # backup, SIIIR sync batch
│
└── docs/
    ├── architecture.md
    ├── api-spec.md
    ├── offline-sync.md
    └── gdpr-compliance.md
```

### 1.1 CLAUDE.md (project-level)

```markdown
# CatalogRO

Digital school catalog platform for Romanian primary, middle, and high schools.

## Stack
- **API:** Go 1.24, chi router, sqlc, goose migrations, River jobs
- **DB:** PostgreSQL 17 with Row-Level Security (multi-tenant via school_id)
- **Web:** Nuxt 3 (SSR), TypeScript strict, Tailwind CSS, Dexie.js (IndexedDB)
- **Infra:** K3S on Hetzner Cloud (EU), Helm, Traefik, cert-manager

## Monorepo layout
- `api/` — Go backend. Run: `cd api && go run ./cmd/server`
- `web/` — Nuxt 3 frontend. Run: `cd web && npm run dev`
- `helm/` — Kubernetes charts

## Database conventions
- All tables have `school_id UUID NOT NULL` for RLS (except `schools`, `districts`)
- RLS policies use `current_setting('app.current_school_id')::uuid`
- Migrations in `api/db/migrations/` using goose (SQL only, no Go migrations)
- Queries in `api/db/queries/` — run `sqlc generate` after changes
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
- **OneRoster 1.2** is the internal data exchange standard. Our entities map to OneRoster:
  - School → Org, Class → Class, Student/Teacher → User, Grade → Result, Subject → Course
  - `source_mappings` table tracks external IDs (SIIIR, OneRoster sourceId) per entity
- **SIIIR import**: Parser-based (NOT API). Reads SIIIR CSV exports (columns: CNP, nume, clasă,
  formă învățământ). Column definitions are versioned in `interop/siiir/columns.go`, not hardcoded.
  Mapping layer converts SIIIR flat data → our normalized schema.
- **SIIIR export**: Reverse mapping for ISJ reporting. Generates CSV in SIIIR-expected format.
- **EIF compliance**: All data exchange uses documented REST APIs (Legea Interoperabilității 2022).
- **EHEIF alignment**: Student data is portable. `interop/portability/` exports a student's full
  record (grades, evaluations, absences, averages) as a structured JSON package that can be
  imported by any OneRoster-compatible system. Supports "student leaves school A, joins school B".
- **API-first**: All interop goes through versioned REST endpoints, never direct DB access.
- Adapter pattern: `interop/registry.go` selects the right parser/exporter based on source format.

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
```

---

## 2. Schema PostgreSQL Inițială

Migrația `001_baseline.sql` — fundația multi-tenant, auth, și structura școlară.

```sql
-- 001_baseline.sql
-- CatalogRO: baseline schema with RLS multi-tenancy

-- ============================================================
-- Extensions
-- ============================================================
CREATE EXTENSION IF NOT EXISTS "pgcrypto";      -- gen_random_uuid()
CREATE EXTENSION IF NOT EXISTS "citext";         -- case-insensitive emails

-- ============================================================
-- ENUM types
-- ============================================================
CREATE TYPE user_role AS ENUM (
    'admin',        -- director/administrator școală
    'secretary',    -- secretariat
    'teacher',      -- profesor (include și diriginte, setat pe clasă)
    'parent',       -- părinte/reprezentant legal
    'student'       -- elev
);

CREATE TYPE education_level AS ENUM (
    'primary',      -- clasele P-IV (pregătitoare + I-IV)
    'middle',       -- clasele V-VIII (gimnaziu)
    'high'          -- clasele IX-XII/XIII (liceu)
);

CREATE TYPE qualifier AS ENUM ('FB', 'B', 'S', 'I');

CREATE TYPE absence_type AS ENUM (
    'unexcused',    -- nemotivată
    'medical',      -- motivată medical
    'excused',      -- motivată (învoitor/alte)
    'school_event'  -- activitate școlară
);

CREATE TYPE sync_status AS ENUM ('pending', 'synced', 'conflict', 'resolved');

CREATE TYPE semester AS ENUM ('I', 'II');

-- ============================================================
-- Districtele (ISJ) — fără RLS, date publice
-- ============================================================
CREATE TABLE districts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,               -- "Inspectoratul Școlar Județean Cluj"
    county_code     CHAR(2) NOT NULL UNIQUE,     -- "CJ", "B", "IF" etc.
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- Școli (tenants)
-- ============================================================
CREATE TABLE schools (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    district_id     UUID NOT NULL REFERENCES districts(id),
    name            TEXT NOT NULL,               -- "Școala Gimnazială Nr. 25"
    siiir_code      TEXT UNIQUE,                 -- cod SIIIR (dacă există)
    education_levels education_level[] NOT NULL,  -- {'primary','middle'} sau {'high'}
    address         TEXT,
    city            TEXT,
    county          TEXT,
    phone           TEXT,
    email           CITEXT,
    config          JSONB NOT NULL DEFAULT '{}', -- configurări specifice școală
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_schools_district ON schools(district_id);
CREATE INDEX idx_schools_siiir ON schools(siiir_code) WHERE siiir_code IS NOT NULL;

-- ============================================================
-- Ani școlari
-- ============================================================
CREATE TABLE school_years (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    label           TEXT NOT NULL,               -- "2026-2027"
    start_date      DATE NOT NULL,
    end_date        DATE NOT NULL,
    sem1_start      DATE NOT NULL,
    sem1_end        DATE NOT NULL,
    sem2_start      DATE NOT NULL,
    sem2_end        DATE NOT NULL,
    is_current      BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(school_id, label)
);

-- ============================================================
-- Utilizatori (provizionați de secretariat, activați de utilizator)
-- ============================================================
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    role            user_role NOT NULL,
    email           CITEXT,
    phone           TEXT,
    first_name      TEXT NOT NULL,
    last_name       TEXT NOT NULL,
    -- Credențiale (NULL până la activare)
    password_hash   TEXT,                        -- bcrypt/argon2, setat la activare
    totp_secret     BYTEA,                       -- encrypted TOTP secret
    totp_enabled    BOOLEAN NOT NULL DEFAULT false,
    -- Provizionare (cine a creat contul)
    provisioned_by  UUID REFERENCES users(id),   -- secretar/admin care a creat contul
    siiir_student_id TEXT,                        -- ID elev din SIIIR (pentru import bulk)
    -- Activare (link one-time trimis pe email/SMS)
    activation_token TEXT UNIQUE,                 -- token hashed (SHA-256), NULL după activare
    activation_sent_at TIMESTAMPTZ,              -- când s-a trimis link-ul
    activated_at    TIMESTAMPTZ,                  -- NULL = cont neactivat (nu poate face login)
    -- GDPR
    gdpr_consent_at TIMESTAMPTZ,                 -- NULL = nu a acceptat încă
    -- Status
    is_active       BOOLEAN NOT NULL DEFAULT true,
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Un email unic per școală per rol
    UNIQUE NULLS NOT DISTINCT (school_id, email, role)
);

CREATE INDEX idx_users_school ON users(school_id);
CREATE INDEX idx_users_email ON users(email) WHERE email IS NOT NULL;
CREATE INDEX idx_users_activation ON users(activation_token) WHERE activation_token IS NOT NULL;
CREATE INDEX idx_users_siiir ON users(siiir_student_id) WHERE siiir_student_id IS NOT NULL;

-- Relație părinte-elev (un părinte poate avea mai mulți copii)
CREATE TABLE parent_student_links (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    parent_id       UUID NOT NULL REFERENCES users(id),
    student_id      UUID NOT NULL REFERENCES users(id),
    relationship    TEXT DEFAULT 'parent',        -- parent, tutor, guardian
    is_primary      BOOLEAN NOT NULL DEFAULT true,-- contactul principal
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(parent_id, student_id)
);

-- ============================================================
-- Clase
-- ============================================================
CREATE TABLE classes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    school_year_id  UUID NOT NULL REFERENCES school_years(id),
    name            TEXT NOT NULL,               -- "5A", "9B", "P" (pregătitoare)
    education_level education_level NOT NULL,
    grade_number    SMALLINT NOT NULL,           -- 0=P, 1-4=primar, 5-8=gim, 9-12=liceu
    homeroom_teacher_id UUID REFERENCES users(id), -- diriginte
    max_students    SMALLINT DEFAULT 30,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(school_id, school_year_id, name)
);

CREATE INDEX idx_classes_school_year ON classes(school_id, school_year_id);

-- Înscrierea elevilor în clase
CREATE TABLE class_enrollments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    class_id        UUID NOT NULL REFERENCES classes(id),
    student_id      UUID NOT NULL REFERENCES users(id),
    enrolled_at     DATE NOT NULL DEFAULT CURRENT_DATE,
    withdrawn_at    DATE,                        -- NULL = activ
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(class_id, student_id)
);

-- ============================================================
-- Materii și încadrare
-- ============================================================
CREATE TABLE subjects (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    name            TEXT NOT NULL,               -- "Matematică", "Limba română"
    short_name      TEXT,                        -- "MAT", "ROM"
    education_level education_level NOT NULL,
    has_thesis      BOOLEAN NOT NULL DEFAULT false, -- are teză?
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(school_id, name, education_level)
);

-- Profesor → Clasă → Materie (cine predă ce la care clasă)
CREATE TABLE class_subject_teachers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    class_id        UUID NOT NULL REFERENCES classes(id),
    subject_id      UUID NOT NULL REFERENCES subjects(id),
    teacher_id      UUID NOT NULL REFERENCES users(id),
    hours_per_week  SMALLINT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(class_id, subject_id, teacher_id)
);

CREATE INDEX idx_cst_teacher ON class_subject_teachers(teacher_id);
CREATE INDEX idx_cst_class ON class_subject_teachers(class_id);

-- ============================================================
-- Configurare reguli evaluare (per școală/nivel)
-- ============================================================
CREATE TABLE evaluation_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    education_level education_level NOT NULL,
    school_year_id  UUID NOT NULL REFERENCES school_years(id),
    -- Reguli evaluare
    use_qualifiers  BOOLEAN NOT NULL DEFAULT false,  -- true pentru primar
    min_grade       SMALLINT NOT NULL DEFAULT 1,
    max_grade       SMALLINT NOT NULL DEFAULT 10,
    thesis_weight   NUMERIC(3,2) DEFAULT 0.25,       -- ponderea tezei în medie
    min_grades_sem  SMALLINT NOT NULL DEFAULT 3,      -- nr. minim note pe semestru
    rounding_rule   TEXT NOT NULL DEFAULT 'standard', -- 'standard', 'round_up', 'round_half_up'
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(school_id, education_level, school_year_id)
);

-- ============================================================
-- SOURCE MAPPINGS (stratul de abstractizare interoperabilitate)
-- Leagă entitățile interne de ID-urile externe (SIIIR, OneRoster, alte sisteme).
-- Permite import/export fără a polua schema principală cu câmpuri specifice fiecărui sistem.
-- ============================================================
CREATE TABLE source_mappings (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    -- Ce entitate internă
    entity_type     TEXT NOT NULL,                -- 'user', 'class', 'subject', 'grade', 'school'
    entity_id       UUID NOT NULL,               -- PK-ul din tabela internă
    -- Sursa externă
    source_system   TEXT NOT NULL,                -- 'siiir', 'oneroster', 'edus', 'csv_import_2026'
    source_id       TEXT NOT NULL,                -- ID-ul din sistemul extern (ex: CNP, SIIIR cod, OneRoster sourcedId)
    source_metadata JSONB DEFAULT '{}',          -- date suplimentare specifice sursei (coloane extra SIIIR etc.)
    -- Tracking
    last_synced_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- O singură mapare per entitate per sursă
    UNIQUE(school_id, entity_type, entity_id, source_system),
    -- Un ID extern unic per sursă per tip
    UNIQUE(school_id, source_system, source_id, entity_type)
);

CREATE INDEX idx_source_mappings_entity ON source_mappings(entity_type, entity_id);
CREATE INDEX idx_source_mappings_source ON source_mappings(source_system, source_id);

-- OneRoster mapping reference:
--   School       → Org       (type: school)
--   Class        → Class     (schoolSourcedId: school.source_id)
--   Subject      → Course    (schoolSourcedId: school.source_id)
--   Student      → User      (role: student)
--   Teacher      → User      (role: teacher)
--   Enrollment   → Enrollment (classSourcedId + userSourcedId)
--   Grade        → Result    (lineItemSourcedId + studentSourcedId)
--   Subject+Sem  → LineItem  (categoryType: grading_period)

-- ============================================================
-- NOTE (nucleul catalogului)
-- ============================================================
CREATE TABLE grades (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    student_id      UUID NOT NULL REFERENCES users(id),
    class_id        UUID NOT NULL REFERENCES classes(id),
    subject_id      UUID NOT NULL REFERENCES subjects(id),
    teacher_id      UUID NOT NULL REFERENCES users(id),    -- cine a pus nota
    school_year_id  UUID NOT NULL REFERENCES school_years(id),
    semester        semester NOT NULL,

    -- Valoare notă: una din cele două coloane va fi NOT NULL
    numeric_grade   SMALLINT CHECK (numeric_grade BETWEEN 1 AND 10),
    qualifier_grade qualifier,
    is_thesis       BOOLEAN NOT NULL DEFAULT false,

    -- Metadate
    grade_date      DATE NOT NULL DEFAULT CURRENT_DATE,
    description     TEXT,                        -- observație opțională
    
    -- Sync offline
    client_id       UUID,                        -- ID generat pe client (dedup)
    client_timestamp TIMESTAMPTZ,                -- timestamp de pe device
    sync_status     sync_status NOT NULL DEFAULT 'synced',

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ,                 -- soft delete

    -- Validare: trebuie să aibă fie notă numerică fie calificativ
    CHECK (
        (numeric_grade IS NOT NULL AND qualifier_grade IS NULL) OR
        (numeric_grade IS NULL AND qualifier_grade IS NOT NULL)
    ),

    -- Deduplicare sync offline
    UNIQUE NULLS NOT DISTINCT (school_id, client_id)
);

CREATE INDEX idx_grades_student ON grades(student_id, subject_id, school_year_id);
CREATE INDEX idx_grades_class ON grades(class_id, subject_id, semester);
CREATE INDEX idx_grades_sync ON grades(sync_status) WHERE sync_status != 'synced';
CREATE INDEX idx_grades_deleted ON grades(deleted_at) WHERE deleted_at IS NOT NULL;

-- ============================================================
-- ABSENȚE
-- ============================================================
CREATE TABLE absences (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    student_id      UUID NOT NULL REFERENCES users(id),
    class_id        UUID NOT NULL REFERENCES classes(id),
    subject_id      UUID NOT NULL REFERENCES subjects(id),
    teacher_id      UUID NOT NULL REFERENCES users(id),    -- cine a marcat
    school_year_id  UUID NOT NULL REFERENCES school_years(id),
    semester        semester NOT NULL,

    absence_date    DATE NOT NULL,
    period_number   SMALLINT NOT NULL,           -- ora (1-7)
    absence_type    absence_type NOT NULL DEFAULT 'unexcused',
    excused_by      UUID REFERENCES users(id),   -- cine a motivat
    excused_at      TIMESTAMPTZ,
    excuse_reason   TEXT,
    excuse_document TEXT,                         -- referință S3/MinIO

    -- Sync offline
    client_id       UUID,
    client_timestamp TIMESTAMPTZ,
    sync_status     sync_status NOT NULL DEFAULT 'synced',

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(student_id, absence_date, period_number),
    UNIQUE NULLS NOT DISTINCT (school_id, client_id)
);

CREATE INDEX idx_absences_student ON absences(student_id, school_year_id);
CREATE INDEX idx_absences_class ON absences(class_id, absence_date);
CREATE INDEX idx_absences_sync ON absences(sync_status) WHERE sync_status != 'synced';

-- ============================================================
-- MEDII (calculate, cache denormalizat)
-- ============================================================
CREATE TABLE averages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    student_id      UUID NOT NULL REFERENCES users(id),
    class_id        UUID NOT NULL REFERENCES classes(id),
    subject_id      UUID NOT NULL REFERENCES subjects(id),
    school_year_id  UUID NOT NULL REFERENCES school_years(id),
    semester        semester,                     -- NULL = medie anuală
    
    computed_value  NUMERIC(4,2),                 -- medie calculată
    final_value     NUMERIC(4,2),                 -- medie finală (poate fi diferită: corigență)
    qualifier_final qualifier,                    -- pentru primar
    is_closed       BOOLEAN NOT NULL DEFAULT false,-- închisă de profesor
    closed_by       UUID REFERENCES users(id),
    closed_at       TIMESTAMPTZ,
    approved_by     UUID REFERENCES users(id),    -- aprobată de director
    approved_at     TIMESTAMPTZ,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(student_id, subject_id, school_year_id, semester)
);

-- ============================================================
-- EVALUĂRI DESCRIPTIVE (primar)
-- ============================================================
CREATE TABLE descriptive_evaluations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    student_id      UUID NOT NULL REFERENCES users(id),
    class_id        UUID NOT NULL REFERENCES classes(id),
    subject_id      UUID NOT NULL REFERENCES subjects(id),
    teacher_id      UUID NOT NULL REFERENCES users(id),
    school_year_id  UUID NOT NULL REFERENCES school_years(id),
    semester        semester NOT NULL,

    content         TEXT NOT NULL,                -- text descriptiv
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- SYNC CONFLICTS (audit)
-- ============================================================
CREATE TABLE sync_conflicts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    entity_type     TEXT NOT NULL,                -- 'grade', 'absence'
    entity_id       UUID NOT NULL,
    client_version  JSONB NOT NULL,               -- ce a trimis clientul
    server_version  JSONB NOT NULL,               -- ce era pe server
    resolution      TEXT NOT NULL DEFAULT 'server_wins', -- 'server_wins','client_wins','manual'
    resolved_by     UUID REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- AUDIT LOG (imutabil)
-- ============================================================
CREATE TABLE audit_log (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    school_id       UUID NOT NULL,
    user_id         UUID NOT NULL,
    action          TEXT NOT NULL,                -- 'grade.create', 'absence.excuse', etc.
    entity_type     TEXT NOT NULL,
    entity_id       UUID NOT NULL,
    old_values      JSONB,
    new_values      JSONB,
    ip_address      INET,
    user_agent      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Partitioned by month pentru performanță pe volume mari
CREATE INDEX idx_audit_school ON audit_log(school_id, created_at DESC);
CREATE INDEX idx_audit_entity ON audit_log(entity_type, entity_id);

-- ============================================================
-- MESAJE
-- ============================================================
CREATE TABLE messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    sender_id       UUID NOT NULL REFERENCES users(id),
    subject         TEXT,
    body            TEXT NOT NULL,
    is_announcement BOOLEAN NOT NULL DEFAULT false, -- anunț școală/clasă
    target_class_id UUID REFERENCES classes(id),     -- NULL = toată școala
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE message_recipients (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       UUID NOT NULL REFERENCES schools(id),
    message_id      UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    recipient_id    UUID NOT NULL REFERENCES users(id),
    read_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(message_id, recipient_id)
);

-- ============================================================
-- SESIUNI (Redis-backed, dar cu fallback PG)
-- ============================================================
CREATE TABLE refresh_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      TEXT NOT NULL UNIQUE,         -- SHA-256 hash
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX idx_refresh_user ON refresh_tokens(user_id);

-- ============================================================
-- ROW-LEVEL SECURITY
-- ============================================================

-- Funcție helper
CREATE OR REPLACE FUNCTION current_school_id() RETURNS UUID AS $$
    SELECT current_setting('app.current_school_id', true)::uuid;
$$ LANGUAGE sql STABLE;

-- Aplicare RLS pe toate tabelele cu school_id
-- (exemplu pentru grades, se repetă pattern-ul pentru fiecare tabel)

ALTER TABLE users ENABLE ROW LEVEL SECURITY;
CREATE POLICY users_tenant ON users USING (school_id = current_school_id());

ALTER TABLE classes ENABLE ROW LEVEL SECURITY;
CREATE POLICY classes_tenant ON classes USING (school_id = current_school_id());

ALTER TABLE class_enrollments ENABLE ROW LEVEL SECURITY;
CREATE POLICY enrollments_tenant ON class_enrollments USING (school_id = current_school_id());

ALTER TABLE subjects ENABLE ROW LEVEL SECURITY;
CREATE POLICY subjects_tenant ON subjects USING (school_id = current_school_id());

ALTER TABLE class_subject_teachers ENABLE ROW LEVEL SECURITY;
CREATE POLICY cst_tenant ON class_subject_teachers USING (school_id = current_school_id());

ALTER TABLE grades ENABLE ROW LEVEL SECURITY;
CREATE POLICY grades_tenant ON grades USING (school_id = current_school_id());

ALTER TABLE absences ENABLE ROW LEVEL SECURITY;
CREATE POLICY absences_tenant ON absences USING (school_id = current_school_id());

ALTER TABLE averages ENABLE ROW LEVEL SECURITY;
CREATE POLICY averages_tenant ON averages USING (school_id = current_school_id());

ALTER TABLE descriptive_evaluations ENABLE ROW LEVEL SECURITY;
CREATE POLICY desc_eval_tenant ON descriptive_evaluations USING (school_id = current_school_id());

ALTER TABLE messages ENABLE ROW LEVEL SECURITY;
CREATE POLICY messages_tenant ON messages USING (school_id = current_school_id());

ALTER TABLE message_recipients ENABLE ROW LEVEL SECURITY;
CREATE POLICY msg_recip_tenant ON message_recipients USING (school_id = current_school_id());

ALTER TABLE school_years ENABLE ROW LEVEL SECURITY;
CREATE POLICY school_years_tenant ON school_years USING (school_id = current_school_id());

ALTER TABLE evaluation_configs ENABLE ROW LEVEL SECURITY;
CREATE POLICY eval_config_tenant ON evaluation_configs USING (school_id = current_school_id());

ALTER TABLE parent_student_links ENABLE ROW LEVEL SECURITY;
CREATE POLICY psl_tenant ON parent_student_links USING (school_id = current_school_id());

ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
CREATE POLICY audit_tenant ON audit_log USING (school_id = current_school_id());

ALTER TABLE sync_conflicts ENABLE ROW LEVEL SECURITY;
CREATE POLICY sync_conflicts_tenant ON sync_conflicts USING (school_id = current_school_id());

-- ============================================================
-- Rolul aplicației (non-superuser, respectă RLS)
-- ============================================================
-- CREATE ROLE catalogro_app LOGIN PASSWORD '...' NOSUPERUSER;
-- GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO catalogro_app;
-- GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO catalogro_app;
```

---

## 3. API Design

### 3.1 Auth Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                  PROVISIONING FLOW                               │
│  (secretariat creează contul cu datele existente)                │
│                                                                 │
│  Admin/Secretary                                                │
│     │                                                           │
│     ├─► POST /api/v1/users                                      │
│     │   { role, first_name, last_name, email?, phone?,          │
│     │     class_id?, parent_links?: [student_id] }              │
│     │   → creates user with activated_at=NULL                   │
│     │   → generates activation_token (hashed in DB)             │
│     │   → sends activation link via email/SMS                   │
│     │                                                           │
│     ├─► POST /api/v1/users/import                (bulk)         │
│     │   { source: "siiir"|"csv", data: [...] }                  │
│     │   → creates users in batch from SIIIR export or CSV       │
│     │   → generates activation links in batch                   │
│     │   → queues email/SMS sending via River job                │
│     │                                                           │
│     └─► POST /api/v1/users/{userId}/resend-activation           │
│         → regenerates token, resends email/SMS                  │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                  ACTIVATION FLOW                                 │
│  (utilizatorul setează credențialele, nu introduce date)         │
│                                                                 │
│  User (clicks link from email/SMS)                              │
│     │                                                           │
│     ├─► GET /api/v1/auth/activate/{token}                       │
│     │   → validates token (not expired, not used)               │
│     │   → returns { school_name, role, first_name, last_name }  │
│     │   → user sees pre-populated data, confirms identity       │
│     │                                                           │
│     ├─► POST /api/v1/auth/activate                              │
│     │   { token, password, gdpr_consent: true (parents) }       │
│     │   → sets password_hash, activated_at=now()                │
│     │   → nullifies activation_token (one-time use)             │
│     │   → if parent: sets gdpr_consent_at, links child visible  │
│     │   │                                                       │
│     │   ├─► [teacher/admin] → returns { mfa_setup_required }    │
│     │   │         │                                             │
│     │   │         ├─► POST /api/v1/auth/2fa/setup               │
│     │   │         │   → returns TOTP secret + QR code           │
│     │   │         │                                             │
│     │   │         └─► POST /api/v1/auth/2fa/verify              │
│     │   │             { totp_code }                             │
│     │   │             → activates 2FA, returns JWT pair         │
│     │   │                                                       │
│     │   └─► [parent/student] → returns JWT pair directly        │
│     │                                                           │
│     └─► Token expired or invalid → show error + link to         │
│         contact school secretary for re-provisioning             │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                      LOGIN FLOW                                  │
│  (conturi deja activate)                                         │
│                                                                 │
│     POST /api/v1/auth/login                                     │
│     { email, password }                                         │
│     │                                                           │
│     ├─► [activated_at IS NULL] → 403 "Account not activated"    │
│     │                                                           │
│     ├─► [no 2FA] → returns { access_token, refresh_token }      │
│     │                                                           │
│     └─► [2FA enabled] → returns { mfa_token, mfa_required }     │
│              │                                                  │
│              └─► POST /api/v1/auth/2fa/login                    │
│                  { mfa_token, totp_code }                       │
│                  → returns { access_token, refresh_token }      │
│                                                                 │
│     POST /api/v1/auth/refresh                                   │
│     { refresh_token }                                           │
│     → rotates tokens, returns new pair                          │
│                                                                 │
│     POST /api/v1/auth/logout                                    │
│     → revokes refresh_token in Redis + PG                       │
└─────────────────────────────────────────────────────────────────┘
```

### 3.2 Tenant Resolution

Fiecare request autentificat conține `school_id` în JWT claims. Middleware-ul:

```
Request → JWT verify → extract school_id → BEGIN TX → SET app.current_school_id → handler → COMMIT
```

Dacă un utilizator are acces la mai multe școli (ex: ISJ), JWT-ul conține lista de school_ids
și un `active_school_id` care se poate schimba via `POST /api/v1/auth/switch-school`.

### 3.3 Endpoints Principale

```
Base URL: /api/v1
Auth: Bearer JWT (header Authorization)
Content-Type: application/json
Pagination: cursor-based (?cursor=xxx&limit=50)
Responses: { "data": {...}, "meta": { "cursor": "..." } }
Errors: { "error": { "code": "GRADE_INVALID", "message": "...", "details": {...} } }
```

#### Catalog (note)

```
GET    /catalog/classes/{classId}/subjects/{subjectId}/grades
        → Lista note per clasă/materie/semestru
        Query: ?semester=I&school_year_id=xxx
        Response: { students: [{ student, grades: [...], average }] }

POST   /catalog/grades
        → Adaugă notă (sau batch de note)
        Body: { student_id, class_id, subject_id, semester,
                numeric_grade|qualifier_grade, grade_date, description?,
                client_id?, client_timestamp? }

PUT    /catalog/grades/{gradeId}
        → Modifică notă existentă (audit trail)

DELETE /catalog/grades/{gradeId}
        → Soft delete (setează deleted_at)

POST   /catalog/grades/sync
        → Sync batch: primește array de mutații offline
        Body: { mutations: [{ action, client_id, client_timestamp, data }] }
        Response: { results: [{ client_id, status, server_id?, conflict? }] }

POST   /catalog/averages/{subjectId}/close
        → Închide mediile pe o materie/clasă/semestru
        Body: { class_id, semester }

POST   /catalog/averages/{averageId}/approve
        → Director aprobă media (workflow)
```

#### Absențe

```
GET    /catalog/classes/{classId}/absences
        → Lista absențe per clasă/dată
        Query: ?date=2026-10-15 sau ?semester=I&month=10

POST   /catalog/absences
        → Marchează absență (singular sau batch pe oră)
        Body: { student_id, class_id, subject_id, absence_date,
                period_number, client_id?, client_timestamp? }

PUT    /catalog/absences/{absenceId}/excuse
        → Motivează absență
        Body: { absence_type, excuse_reason?, excuse_document? }

POST   /catalog/absences/sync
        → Sync batch analog cu grades
```

#### Evaluări descriptive (primar)

```
GET    /catalog/classes/{classId}/subjects/{subjectId}/evaluations
        → Fișe descriptive per clasă/materie

POST   /catalog/evaluations
        Body: { student_id, class_id, subject_id, semester, content }

PUT    /catalog/evaluations/{evalId}
```

#### Școală & configurare

```
GET    /schools/current                → Detalii școală curentă
PUT    /schools/current                → Update configurare
GET    /schools/current/year           → An școlar curent
POST   /schools/years                  → Creare an școlar nou

GET    /classes                        → Lista clase (an curent)
POST   /classes                        → Creare clasă
GET    /classes/{classId}              → Detalii clasă cu elevi
PUT    /classes/{classId}              → Update clasă
POST   /classes/{classId}/enroll       → Înscrie elevi
DELETE /classes/{classId}/enroll/{studentId} → Retrage elev

GET    /subjects                       → Lista materii
POST   /subjects                       → Creare materie
GET    /classes/{classId}/teachers     → Încadrare per clasă
POST   /classes/{classId}/teachers     → Asignare profesor-materie
```

#### Utilizatori (provizionare + gestiune)

```
GET    /users/me                       → Profil curent (date pre-populate)
PUT    /users/me                       → Update preferințe (NU date identitate)
GET    /users                          → Lista utilizatori (admin/secretary)
POST   /users                          → Provizionare cont (admin/secretary)
                                          Body: { role, first_name, last_name,
                                                  email?, phone?, class_id?,
                                                  parent_links?: [student_id] }
POST   /users/import                   → Import bulk (SIIIR/CSV)
POST   /users/{userId}/resend-activation → Retrimite link activare
PUT    /users/{userId}                 → Update date utilizator (admin/secretary)
POST   /users/{userId}/deactivate     → Dezactivare cont
GET    /users/me/children              → Copiii mei (rol parent)
GET    /users/pending                  → Conturi neactivate (admin/secretary)

GET    /auth/activate/{token}          → Validare token + date pre-populate
POST   /auth/activate                  → Setare parolă + GDPR consent
POST   /auth/2fa/setup                 → Configurare TOTP (obligatoriu prof/admin)
POST   /auth/2fa/verify                → Verificare cod TOTP

POST   /users/me/gdpr/consent         → Acceptare GDPR (dacă nu s-a făcut la activare)
POST   /users/me/gdpr/export          → Export date personale (GDPR Art.20)
POST   /users/me/gdpr/delete          → Cerere ștergere (GDPR Art.17)
```

#### Mesagerie

```
GET    /messages                       → Inbox (paginated)
GET    /messages/{messageId}           → Detalii mesaj
POST   /messages                       → Trimite mesaj
POST   /messages/announcements         → Anunț clasă/școală (admin/teacher)
PUT    /messages/{messageId}/read      → Marchează citit
```

#### Rapoarte

```
POST   /reports/catalog-pdf            → Generare catalog tipărit (async → job ID)
GET    /reports/jobs/{jobId}           → Status job + download link
GET    /reports/dashboard              → Date dashboard director
GET    /reports/student/{studentId}    → Fișa completă elev
GET    /reports/class/{classId}/stats  → Statistici clasă
POST   /reports/isj-export             → Export format ISJ
```

#### Sync (endpoint central offline)

```
POST   /sync/push
        → Flush mutații offline (grades + absences mixed)
        Body: {
            device_id: "uuid",
            last_sync_at: "2026-10-15T08:30:00Z",
            mutations: [
                { type: "grade", action: "create", client_id, client_timestamp, data },
                { type: "absence", action: "create", client_id, client_timestamp, data },
                ...
            ]
        }
        Response: {
            results: [{ client_id, status, server_id?, conflict? }],
            server_timestamp: "2026-10-15T10:00:00Z"
        }

GET    /sync/pull
        → Pull changes since last sync
        Query: ?since=2026-10-15T08:30:00Z&classes=id1,id2
        Response: {
            grades: [{ ...changed grades }],
            absences: [{ ...changed absences }],
            server_timestamp: "..."
        }
```

#### Interoperabilitate (import/export/OneRoster)

```
POST   /interop/import                 → Import generic (detectare automată format)
        Body: multipart/form-data { file, source_hint?: "siiir"|"oneroster"|"csv" }
        Response: { preview: { students: 127, teachers: 14, classes: 6 },
                    import_id: "uuid", status: "pending_confirmation" }

POST   /interop/import/{importId}/confirm → Confirmă importul după preview
        Response: { created: 127, updated: 3, skipped: 2, errors: [] }

GET    /interop/import/{importId}/status → Status import (pentru import-uri mari, async)

POST   /interop/export/siiir            → Export în format SIIIR (raportare ISJ)
        Body: { school_year_id, semester?, entity_types: ["students","grades"] }
        Response: CSV file download

POST   /interop/portability/export/{studentId} → Export Student Record Package (EHEIF)
        Response: JSON structured package

POST   /interop/portability/import      → Import Student Record Package (elev transferat)
        Body: JSON package
        Response: { student_id, source_mappings_created: 3 }

GET    /interop/source-mappings         → Lista mapări entități ↔ sisteme externe
        Query: ?entity_type=user&source_system=siiir

--- OneRoster 1.2 endpoints (read-only, autentificare via API key) ---

GET    /oneroster/orgs                  → Organizații (școli + inspectorate)
GET    /oneroster/orgs/{sourcedId}
GET    /oneroster/users                 → Utilizatori (filtrare per rol)
GET    /oneroster/users/{sourcedId}
GET    /oneroster/classes               → Clase
GET    /oneroster/classes/{sourcedId}/students
GET    /oneroster/courses               → Materii
GET    /oneroster/enrollments           → Înscrieri
GET    /oneroster/academicSessions      → Ani școlari + semestre
GET    /oneroster/lineItems             → Elemente evaluare (materie+semestru)
GET    /oneroster/results               → Note/rezultate

--- Discovery ---

GET    /.well-known/openapi.json        → Specificație OpenAPI 3.1 (fără auth)
```

### 3.4 Rate Limiting & Headers Standard

```
Rate limits:
  - Auth endpoints:     10 req/min per IP
  - Sync endpoints:     30 req/min per user
  - Read endpoints:    120 req/min per user
  - Write endpoints:    60 req/min per user
  - OneRoster API:      60 req/min per API key
  - Interop import:      5 req/hour per school (prevent abuse)

Response headers:
  X-Request-Id:         UUID per request (pentru debugging)
  X-RateLimit-Remaining: requests rămase
  X-School-Id:          tenant curent (debugging)

CORS:
  Access-Control-Allow-Origin: https://app.catalogro.ro (prod)
  Access-Control-Allow-Credentials: true
```

### 3.5 Webhook-uri pentru notificări

```
Intern (River jobs triggered by DB events):

user.provisioned  → email/SMS cu link activare
user.bulk_import  → batch email/SMS activare (throttled)
grade.created     → notificare push către părinte
absence.created   → notificare push către părinte
absence.excused   → notificare către profesor care a marcat
average.closed    → notificare către diriginte
message.sent      → notificare push către destinatari
```

---

## 4. Offline Sync — Design Detaliat

### 4.1 IndexedDB Schema (Dexie.js)

```typescript
// web/lib/db.ts
import Dexie, { type Table } from 'dexie';

interface CachedGrade {
  id: string;           // server ID (sau client_id dacă nu e sincronizat)
  serverId?: string;
  studentId: string;
  classId: string;
  subjectId: string;
  semester: 'I' | 'II';
  numericGrade?: number;
  qualifierGrade?: 'FB' | 'B' | 'S' | 'I';
  isThesis: boolean;
  gradeDate: string;
  description?: string;
  updatedAt: string;
}

interface CachedAbsence { /* analog */ }

interface SyncMutation {
  id?: number;          // auto-increment
  clientId: string;     // UUID v4, generat pe client
  entityType: 'grade' | 'absence';
  action: 'create' | 'update' | 'delete';
  data: Record<string, unknown>;
  clientTimestamp: string;
  attempts: number;
  lastError?: string;
  status: 'pending' | 'syncing' | 'failed';
  createdAt: string;
}

interface SyncMeta {
  key: string;          // 'lastSyncAt', 'deviceId'
  value: string;
}

class CatalogDB extends Dexie {
  grades!: Table<CachedGrade>;
  absences!: Table<CachedAbsence>;
  syncQueue!: Table<SyncMutation>;
  syncMeta!: Table<SyncMeta>;

  constructor() {
    super('catalogro');
    this.version(1).stores({
      grades: 'id, [classId+subjectId+semester], studentId, updatedAt',
      absences: 'id, [classId+absenceDate], studentId, updatedAt',
      syncQueue: '++id, status, entityType, createdAt',
      syncMeta: 'key',
    });
  }
}
```

### 4.2 Sync Queue Flow

```
┌─────────────┐     ┌──────────────┐     ┌────────────┐     ┌──────────┐
│  User adds  │────►│ Write to     │────►│ Add to     │────►│ Try sync │
│  grade in   │     │ IndexedDB    │     │ syncQueue  │     │ if online│
│  GradeGrid  │     │ (optimistic) │     │ (pending)  │     │          │
└─────────────┘     └──────────────┘     └────────────┘     └─────┬────┘
                                                                    │
                    ┌──────────────────────────────────────────────┘
                    │
              ┌─────▼─────┐     ┌──────────────┐     ┌────────────────┐
              │ POST       │────►│ Server       │────►│ Update local   │
              │ /sync/push │     │ processes    │     │ IDs, clear     │
              │            │     │ + audit log  │     │ queue entry    │
              └────────────┘     └──────┬───────┘     └────────────────┘
                                        │
                                  ┌─────▼──────┐
                                  │ Conflict?  │
                                  │            │
                                  ├─YES──► log to sync_conflicts
                                  │        keep server version
                                  │        notify user
                                  │
                                  └─NO───► synced ✓
```

---

## 5. Arhitectura de Interoperabilitate

### 5.1 Principiul fundamental: Adapter Pattern

CatalogRO NU se leagă direct de niciun format extern. Între modelul intern și lumea exterioară
există un strat de adaptoare (adapters) care traduce bidirecțional:

```
┌──────────────────────────────────────────────────────────────────┐
│                    LUMEA EXTERIOARĂ                               │
│                                                                  │
│  ┌─────────────┐  ┌─────────────┐  ┌──────────────┐             │
│  │ SIIIR CSV   │  │ OneRoster   │  │ Alte sisteme │             │
│  │ (export MEN)│  │ 1.2 JSON/CSV│  │ (EDUS, 24edu)│             │
│  └──────┬──────┘  └──────┬──────┘  └──────┬───────┘             │
│         │                │                │                      │
└─────────┼────────────────┼────────────────┼──────────────────────┘
          │                │                │
      ┌────▼────┐     ┌────▼────┐      ┌────▼────┐
      │ SIIIR   │     │OneRoster│      │ Custom  │
      │ Adapter │     │ Adapter │      │ Adapter │
      │         │     │         │      │         │
      │ parser  │     │ import  │      │ parser  │
      │ mapper  │     │ export  │      │ mapper  │
      │ exporter│     │ handler │      │         │
      └────┬────┘     └────┬────┘      └────┬────┘
          │                │                │
          └────────┬───────┘────────────────┘
                    │
          ┌────────▼─────────┐
          │   Registry       │
          │   (selectează    │
          │    adapterul     │
          │    potrivit)     │
          └────────┬─────────┘
                    │
      ┌─────────────▼──────────────────────────────────┐
      │            MODEL INTERN CatalogRO               │
      │                                                 │
      │  users · classes · subjects · grades · absences │
      │              source_mappings                     │
      │  (legătura: entity_id ←→ source_system:source_id)│
      └─────────────────────────────────────────────────┘
```

### 5.2 SIIIR: Import prin parser CSV

SIIIR nu are API public. Soluțiile existente din piață (24edu, CASESoftware etc.) au
construit parser-e care recunosc automat coloanele din exporturile CSV generate de SIIIR.

CatalogRO face la fel, dar cu un strat de abstractizare:

```go
// interop/siiir/columns.go — definiții versionabile, NU hardcodate
type SIIIRColumnMapping struct {
    Version     string            // "2024-v1", "2025-v1"
    Columns     map[string]string // "student_cnp" → coloana 3, "class_name" → coloana 7
    Delimiters  []string          // [",", ";", "\t"] — SIIIR nu e consistent
    Encoding    string            // "UTF-8", "Windows-1250" — exporturile vechi sunt ANSI
}

// interop/siiir/parser.go — recunoaște automat versiunea
func DetectVersion(reader io.Reader) (*SIIIRColumnMapping, error)
func ParseStudents(reader io.Reader, mapping *SIIIRColumnMapping) ([]SIIIRStudent, error)
func ParseTeachers(reader io.Reader, mapping *SIIIRColumnMapping) ([]SIIIRTeacher, error)

// interop/siiir/mapper.go — conversie SIIIR → model intern
func (m *Mapper) StudentToUser(s SIIIRStudent) (*model.User, *model.SourceMapping, error)
func (m *Mapper) ClassToClass(s SIIIRClass) (*model.Class, *model.SourceMapping, error)
```

**Flow practic:**
1. Secretariatul descarcă CSV din SIIIR (sau din interfața ISJ)
2. Uploadează fișierul în CatalogRO → `POST /api/v1/interop/import`
3. Parser-ul detectează automat formatul, coloanele, encoding-ul
4. Sistemul afișează un preview: "Am găsit 127 elevi, 14 profesori, 6 clase"
5. Secretariatul confirmă → datele se creează cu `source_mappings` care leagă fiecare
  entitate de ID-ul SIIIR original (CNP, cod SIIIR etc.)
6. La re-import, `source_mappings` previne duplicatele — se face update, nu insert

### 5.3 OneRoster 1.2: Standardul de date

OneRoster (IMS Global) definește un model de date și un API REST pentru schimbul de
informații între sisteme educaționale. CatalogRO adoptă OneRoster ca limbaj comun intern:

**Mapping entități:**

```
CatalogRO           → OneRoster 1.2
──────────────────────────────────────
schools             → Orgs          (type: school)
districts           → Orgs          (type: district)
classes             → Classes       (schoolSourcedId)
subjects            → Courses       (schoolSourcedId)
users (teacher)     → Users         (role: teacher)
users (student)     → Users         (role: student)
users (parent)      → Users         (role: guardian)
class_enrollments   → Enrollments   (classSourcedId + userSourcedId)
grades              → Results       (lineItemSourcedId + studentSourcedId)
subject+semester    → LineItems     (categoryType: term)
school_years        → AcademicSessions (type: schoolYear / term)
```

**Endpoints OneRoster (read-only inițial, V1.0+):**

```
GET /api/v1/oneroster/orgs                    → Lista școli
GET /api/v1/oneroster/orgs/{id}
GET /api/v1/oneroster/users                   → Elevi + profesori
GET /api/v1/oneroster/users/{id}
GET /api/v1/oneroster/classes                 → Clase
GET /api/v1/oneroster/classes/{id}/students
GET /api/v1/oneroster/courses                 → Materii
GET /api/v1/oneroster/enrollments             → Înscrieri
GET /api/v1/oneroster/lineItems               → Elemente evaluare
GET /api/v1/oneroster/results                 → Note/rezultate
GET /api/v1/oneroster/academicSessions        → Ani școlari + semestre
```

Răspunsurile respectă formatul OneRoster JSON Binding:
```json
{
  "user": {
    "sourcedId": "uuid",
    "status": "active",
    "givenName": "Andrei",
    "familyName": "Moldovan",
    "role": "student",
    "orgs": [{ "sourcedId": "school-uuid", "type": "school" }]
  }
}
```

### 5.4 EHEIF: Portabilitatea datelor elevului

European Higher Education Interoperability Framework (2025) promovează principiul
"datele urmează elevul". CatalogRO implementează asta prin:

**Student Record Package** — un export JSON structurat cu tot parcursul elevului:

```json
{
  "version": "1.0",
  "standard": "catalogro-student-record",
  "oneroster_compatible": true,
  "exported_at": "2027-06-20T10:00:00Z",
  "student": {
    "sourcedId": "uuid",
    "givenName": "Alexandru",
    "familyName": "Pop",
    "identifier": "siiir:RO-CJ-12345"
  },
  "school_history": [
    {
      "school": { "name": "Școala Gimnazială Liviu Rebreanu", "siiir_code": "CJ-GIM-0042" },
      "period": { "from": "2023-09-11", "to": "2027-06-20" },
      "classes": ["5B", "6B", "7B", "8B"],
      "academic_records": [
        {
          "year": "2026-2027", "class": "6B", "semester": "I",
          "results": [
            { "course": "Matematică", "grade": 9.50, "type": "semester_average" },
            { "course": "Limba română", "grade": 8.75, "type": "semester_average" }
          ],
          "absences": { "total": 12, "excused": 8, "unexcused": 4 }
        }
      ]
    }
  ]
}
```

**Flow transfer elev:**
1. Școala A exportă Student Record Package → `POST /api/v1/interop/portability/export/{studentId}`
2. Părintele primește fișierul JSON (sau link securizat cu expirare)
3. Școala B importă pachetul → `POST /api/v1/interop/portability/import`
4. Sistemul creează elevul cu istoric complet + `source_mappings` către școala veche

### 5.5 EIF (European Interoperability Framework): Conformitate

Legea Interoperabilității din România (2022) obligă instituțiile publice să permită
schimbul de date prin interfețe documentate. CatalogRO respectă EIF prin:

- **API-first**: Orice funcționalitate accesibilă prin UI e accesibilă și prin API REST documentat
- **Formatul deschis**: Export în JSON, CSV, PDF — fără formate proprietare
- **Endpoint de discovery**: `GET /api/v1/.well-known/openapi.json` — specificație OpenAPI 3.1
- **Versionare API**: Prefix `/v1/`, backward compatibility pe durata unei versiuni majore
- **Autentificare standard**: OAuth2 / API keys pentru integrări machine-to-machine
- **source_mappings**: Orice entitate poate fi referită prin ID-ul din orice sistem extern

---

## 6. Pași de Pornire Concreți

### Săptămâna 1-2: Foundation

```
1. Init monorepo + CLAUDE.md
2. docker-compose.yml (PG 17 + Redis 7 + Mailpit)
3. Go module init + chi router skeleton
4. Migrația 001_baseline.sql (schema de mai sus, inclusiv source_mappings)
5. sqlc.yaml + primele query files (schools, users, source_mappings)
6. Auth flow complet: provizionare conturi + activare + login + refresh + 2FA
7. RLS middleware testat cu 2 școli de test
```

### Săptămâna 3-4: Catalog Core

```
8. Nuxt 3 init + PWA config + Dexie.js setup
9. Endpoints CRUD grade + absences
10. GradeGrid.vue — componenta principală de editare
11. Sync queue (offline mutations → /sync/push)
12. Calcul medii (evaluation_configs engine)
13. Notificări push (Web Push VAPID)
```

### Săptămâna 5-6: Import SIIIR & Polish

```
14. Seed data: 2 școli (primar + liceu), date realiste + conturi pre-provizionate
15. SIIIR parser: detectare automată format CSV, preview, import cu source_mappings
16. Import bulk utilizatori din fișier SIIIR (flow complet cu confirmare)
17. Dashboard per rol (profesor, părinte, admin)
18. Mesagerie de bază
19. Catalog PDF export
20. E2E tests (Playwright): provizionare → activare → login → notă + import SIIIR
21. Deploy staging pe K3S
```

### Săptămâna 7-8 (MVP+): Interoperabilitate

```
22. Endpoints OneRoster read-only (Rostering: orgs, users, classes, enrollments)
23. Endpoints OneRoster Gradebook (lineItems, results)
24. Student Record Package export (EHEIF-aligned)
25. OpenAPI 3.1 spec auto-generated din handler annotations
26. Documentație API publică
```
