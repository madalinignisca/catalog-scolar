# E2E Playwright Test Suite — Design Spec

**Date:** 2026-03-23
**Status:** Draft
**Scope:** Exhaustive Playwright E2E tests for all 5 user roles across all implemented pages

## 1. Goals

Build a comprehensive E2E test suite (~73 tests) that:

- Validates every user role (admin, secretary, teacher, parent, student) can authenticate and see role-appropriate content
- Tests the complete teacher grading workflow (CRUD on grades) for both primary (qualifiers) and middle/high (numeric) education levels
- Verifies role-based access control boundaries (e.g., parent cannot access teacher routes)
- Covers offline sync behavior, error states, session management, and edge cases
- Runs against the real backend (full integration — no API mocking)

## 2. Decisions

### 2.1 MFA Strategy: Real TOTP codes in tests

Admin, secretary, and teacher roles require 2FA (TOTP). Tests generate valid TOTP codes at runtime using the `otpauth` npm library with known secrets seeded in the database.

**Seed data changes required:**
- Add `totp_secret` and `totp_enabled = true` for admin, secretary, and all teachers in `seed.sql`
- Use a known base32-encoded secret (e.g., `JBSWY3DPEHPK3PXP`) that tests can reference
- The API's TOTP encryption must be compatible — if secrets are encrypted at rest, seed must store the encrypted form

### 2.2 Data Strategy: Fresh database per test run

- Playwright `globalSetup` runs `make migrate` and `make seed` before the suite starts
- Tests can freely create/modify data (grades, absences) without cleanup
- No per-test teardown needed — the DB is disposable
- Sequential test execution (workers: 1) prevents race conditions

### 2.3 Test Scope: Exhaustive

All implemented pages, all roles, plus offline sync, error states, token lifecycle, and edge cases.

### 2.4 Seed Data Alignment

The current `auth.fixture.ts` uses placeholder emails (`admin@scoala-test.ro`, etc.) that do not match the actual seed data. The fixture will be updated to use real seed credentials:

| Role      | Email                                | Password     | TOTP   | UUID                                   |
|-----------|--------------------------------------|--------------|--------|----------------------------------------|
| admin     | `director@scoala-rebreanu.ro`        | `catalog2026`| Yes    | `b1000000-0000-0000-0000-000000000001` |
| secretary | `secretar@scoala-rebreanu.ro`        | `catalog2026`| Yes    | `b1000000-0000-0000-0000-000000000002` |
| teacher   | `ana.dumitrescu@scoala-rebreanu.ro`  | `catalog2026`| Yes    | `b1000000-0000-0000-0000-000000000010` |
| parent    | `ion.moldovan@gmail.com`             | `catalog2026`| No     | `b1000000-0000-0000-0000-000000000301` |
| student   | `andrei.moldovan@elev.rebreanu.ro`   | `catalog2026`| No     | `b1000000-0000-0000-0000-000000000101` |

**Student activation gap:** Students in seed have `activated_at` but no `password_hash`. The seed must be updated to add `password_hash` for at least 2 students (Andrei Moldovan in 2A, Alexandru Pop in 6B). Remaining students stay unactivated for activation flow tests.

### 2.5 Unactivated Student for Activation Tests

Keep student `radu.campean@elev.rebreanu.ro` (UUID `...0205`) without `password_hash` and with an `activation_token` set, so the activation flow can be tested end-to-end.

## 3. Infrastructure

### 3.1 File Structure

```
web/test/e2e/
  global-setup.ts              -- Fresh DB: make migrate + make seed + health check
  fixtures/
    auth.fixture.ts            -- Role-based fixtures with real TOTP (rewritten)
  helpers/
    totp.ts                    -- generateTOTP(secret) -> 6-digit code
  page-objects/
    login.page.ts              -- (exists — enhance with MFA methods)
    dashboard.page.ts          -- Role-aware dashboard assertions
    catalog.page.ts            -- Semester toggle, subject tabs, grade grid
    grade-input.page.ts        -- Add/edit grade modal
    activation.page.ts         -- Multi-step activation flow
    layout.page.ts             -- Sidebar, navigation, responsive
  auth/
    login.spec.ts              -- (exists — replace skipped tests, add all roles)
    token.spec.ts              -- Token refresh/expiry tests
    activation.spec.ts         -- Account activation flow
    access-control.spec.ts     -- Role-based access denial
  dashboard/
    teacher.spec.ts            -- Teacher class grid
    admin.spec.ts              -- Admin quick-access cards
    parent.spec.ts             -- Parent children view
    student.spec.ts            -- Student welcome view
  navigation/
    sidebar.spec.ts            -- Sidebar nav items, active state, user info
    responsive.spec.ts         -- Mobile hamburger, overlay sidebar
  catalog/
    navigation.spec.ts         -- Class header, semester toggle, subject tabs
    grade-grid.spec.ts         -- Grade display, colors, tooltips, sorting
    grade-crud.spec.ts         -- Add/edit/delete grades, validation
    grade-edge-cases.spec.ts   -- Thesis grades, empty states, edge cases
  sync/
    offline-mode.spec.ts       -- Offline indicator, optimistic updates, reconnect
    conflict.spec.ts           -- Offline grade creation persists after sync
  error/
    api-errors.spec.ts         -- 500, 403 error handling
    session.spec.ts            -- Session expiry, stale token refresh
  edge/
    empty-states.spec.ts       -- No classes, no grades, empty semester
```

### 3.2 Global Setup (`global-setup.ts`)

Runs before any test. Responsibilities:

1. Reset database: run `make migrate` then `make seed` from the project root
2. Wait for API health: poll `GET http://localhost:8080/api/v1/health` with 30s timeout
3. Wait for Nuxt dev server: poll `http://localhost:3000` with 60s timeout

Uses Node.js `child_process.execFileSync` (not `exec`) to avoid shell injection. Commands are split into binary + args arrays.

### 3.3 TOTP Helper (`helpers/totp.ts`)

Uses the `otpauth` npm library (devDependency) to generate valid 6-digit TOTP codes.

- Exports a constant `TEST_TOTP_SECRET` (base32) matching the seed data
- Exports `generateTOTP(secret?: string): string` that returns a valid code for the current 30-second window
- Used by auth fixtures for admin, secretary, and teacher login

### 3.4 Auth Fixtures (`fixtures/auth.fixture.ts`)

Each role fixture performs a real login:
1. Navigate to `/login`
2. Fill email + password from seed credentials
3. Submit the form
4. If MFA required (admin, secretary, teacher): generate TOTP code, fill MFA input, submit
5. Wait for redirect to dashboard (`/`)
6. Hand the authenticated `Page` to the test

Exports: `adminPage`, `secretaryPage`, `teacherPage`, `parentPage`, `studentPage`

### 3.5 Playwright Config Changes

- Add `globalSetup: './test/e2e/global-setup.ts'` to `playwright.config.ts`
- Keep workers: 1 (sequential — DB mutations in tests)
- Keep existing dual-mode (local dev + CI) setup

### 3.6 Page Objects

Each page object encapsulates locators (via `data-testid`) and common actions:

- **LoginPage** (enhance existing): `fillEmail()`, `fillPassword()`, `fillMfaCode()`, `submit()`, `getError()`, `getMfaError()`, `isOnDashboard()`
- **DashboardPage**: `getClassCards()`, `getAdminCards()`, `getWelcomeMessage()`, `clickClassCard(name)`, `getUserRole()`
- **CatalogPage**: `getClassHeader()`, `selectSemester(sem)`, `getSubjectTabs()`, `clickSubjectTab(name)`, `getStudentRows()`, `clickAddGrade(studentName)`, `clickGradeBadge(studentName, index)`, `getAverage(studentName)`, `goBack()`
- **GradeInputModal**: `isVisible()`, `selectQualifier(q)`, `fillNumericGrade(n)`, `setDate(date)`, `fillDescription(text)`, `save()`, `close()`, `getValidationErrors()`
- **ActivationPage**: `getUserInfo()`, `fillPassword(pw)`, `fillPasswordConfirm(pw)`, `acceptGdpr()`, `submit()`, `getMfaQrCode()`, `fillMfaSetupCode(code)`, `getSuccessMessage()`, `getError()`
- **LayoutPage**: `getSidebarItems()`, `getActiveNavItem()`, `getUserInfo()`, `clickLogout()`, `isHamburgerVisible()`, `openMobileMenu()`, `closeMobileMenu()`, `isSidebarVisible()`, `getSyncStatus()`

## 4. Test Plan

### 4.1 Authentication and Login (~10 tests)

**File: `auth/login.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 1 | Login page renders email, password fields, submit button | none |
| 2 | Empty form submission shows HTML5 validation | none |
| 3 | Invalid credentials show error message | none |
| 4 | Parent login succeeds (no MFA) -> redirects to dashboard | none |
| 5 | Student login succeeds (no MFA) -> redirects to dashboard | none |
| 6 | Teacher login: MFA step appears -> valid TOTP -> dashboard | none |
| 7 | Teacher login: invalid TOTP code shows error | none |
| 8 | Admin login with MFA succeeds | none |
| 9 | Secretary login with MFA succeeds | none |
| 10 | Logout clears session -> redirects to login | parentPage |

### 4.2 Token Lifecycle (~3 tests)

**File: `auth/token.spec.ts`**

| # | Test | Notes |
|---|------|-------|
| 11 | Expired access token triggers silent refresh -> page works | Manipulate localStorage token |
| 12 | Expired refresh token -> redirects to login | Clear refresh token |
| 13 | Direct navigation to `/` without token -> redirects to `/login` | Fresh page, no auth |

### 4.3 Account Activation (~4 tests)

**File: `auth/activation.spec.ts`**

| # | Test | Notes |
|---|------|-------|
| 14 | Valid activation token -> shows user info + password form | Use seeded unactivated student |
| 15 | Invalid/expired token -> shows error | Random token |
| 16 | Complete activation: password -> GDPR (if parent) -> 2FA (if required) -> success | Full flow |
| 17 | Password validation: too short, mismatch -> shows errors | Boundary testing |

### 4.4 Access Control (~3 tests)

**File: `auth/access-control.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 18 | Parent navigating to `/catalog/{classId}` -> error/redirect | parentPage |
| 19 | Student navigating to admin routes -> denied | studentPage |
| 20 | Teacher accessing unassigned class -> denied/empty | teacherPage |

### 4.5 Teacher Dashboard (~3 tests)

**File: `dashboard/teacher.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 21 | Teacher sees grid of assigned class cards only | teacherPage |
| 22 | Class card shows: name, level badge, student count, subjects | teacherPage |
| 23 | Clicking class card navigates to `/catalog/{classId}` | teacherPage |

### 4.6 Admin Dashboard (~2 tests)

**File: `dashboard/admin.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 24 | Admin sees quick-access cards (users, classes, reports) | adminPage |
| 25 | Admin cards link to correct routes | adminPage |

### 4.7 Parent Dashboard (~2 tests)

**File: `dashboard/parent.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 26 | Parent sees "My children" section with linked students | parentPage |
| 27 | Parent does NOT see teacher class grid or admin cards | parentPage |

### 4.8 Student Dashboard (~2 tests)

**File: `dashboard/student.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 28 | Student sees welcome message with own name | studentPage |
| 29 | Student does NOT see class management or admin features | studentPage |

### 4.9 Navigation and Sidebar (~4 tests)

**File: `navigation/sidebar.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 30 | Sidebar shows correct nav items (Tablou de bord, Catalog, Absente) | teacherPage |
| 31 | Active route is highlighted in sidebar | teacherPage |
| 32 | User info displayed in sidebar footer (name, role) | teacherPage |
| 33 | Logout button in header clears session | teacherPage |

### 4.10 Responsive Layout (~4 tests)

**File: `navigation/responsive.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 34 | Mobile viewport: sidebar hidden, hamburger visible | teacherPage |
| 35 | Clicking hamburger opens sidebar overlay | teacherPage |
| 36 | Clicking backdrop/close dismisses sidebar | teacherPage |
| 37 | Navigation works on mobile -> page loads | teacherPage |

### 4.11 Catalog Page Structure (~6 tests)

**File: `catalog/navigation.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 38 | Class page shows header (name, level, count, back link) | teacherPage |
| 39 | Semester toggle defaults to "Semestrul I" | teacherPage |
| 40 | Switching semester reloads grade data | teacherPage |
| 41 | Subject tabs render for teacher's assigned subjects | teacherPage |
| 42 | Clicking subject tab loads that subject's grades | teacherPage |
| 43 | Back link returns to dashboard | teacherPage |

### 4.12 Grade Display (~7 tests)

**File: `catalog/grade-grid.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 44 | Students sorted alphabetically by last name | teacherPage |
| 45 | Rows show: number, name, grade badges, average, add button | teacherPage |
| 46 | Primary class: qualifier badges (FB/B/S/I) with correct colors | teacherPage |
| 47 | Middle class: numeric badges with correct color ranges | teacherPage |
| 48 | Hovering grade shows tooltip (date + description) | teacherPage |
| 49 | Thesis grades display "T" prefix + purple ring | teacherPage |
| 50 | Loading state shows skeleton animation | teacherPage |

### 4.13 Grade CRUD (~6 tests)

**File: `catalog/grade-crud.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 51 | Add qualifier grade (primary): click add -> modal -> select FB -> save -> appears | teacherPage |
| 52 | Add numeric grade (middle): click add -> modal -> enter 8 -> save -> appears | teacherPage |
| 53 | Edit grade: click badge -> modal pre-filled -> change -> save -> updates | teacherPage |
| 54 | Delete grade: hover -> delete -> confirm -> removed | teacherPage |
| 55 | Validation: numeric 1-10, date required, qualifier required for primary | teacherPage |
| 56 | Average recalculates after grade CRUD (middle/high only) | teacherPage |

### 4.14 Grade Edge Cases (~3 tests)

**File: `catalog/grade-edge-cases.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 57 | Thesis grade displays with "T" prefix after creation | teacherPage |
| 58 | Multiple grades per student render in sequence | teacherPage |
| 59 | Grade description is optional — saves without it | teacherPage |

### 4.15 Offline Sync (~6 tests)

**File: `sync/offline-mode.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 60 | SyncStatus shows green "Sincronizat" when online, no pending | teacherPage |
| 61 | Going offline -> yellow "Offline" indicator | teacherPage |
| 62 | Adding grade while offline -> appears optimistically + sync count | teacherPage |
| 63 | Coming back online -> sync flushes -> green "Sincronizat" | teacherPage |
| 64 | Multiple offline mutations -> count increments correctly | teacherPage |

**File: `sync/conflict.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 65 | Offline grade syncs successfully -> persists after page reload | teacherPage |

### 4.16 Error States (~4 tests)

**File: `error/api-errors.spec.ts`**

| # | Test | Notes |
|---|------|-------|
| 66 | API 500 on grade fetch -> error banner in grid | Route intercept for this specific call |
| 67 | API 403 on grade creation -> error message | Attempt unauthorized action |
| 68 | Network timeout on login -> error displayed | Route intercept with abort |

**File: `error/session.spec.ts`**

| # | Test | Notes |
|---|------|-------|
| 69 | Session expires during use -> redirect to login | Clear refresh token mid-session |
| 70 | Stale access token + valid refresh -> silent refresh -> works | Manipulate localStorage |

### 4.17 Empty/Edge States (~3 tests)

**File: `edge/empty-states.spec.ts`**

| # | Test | Fixture |
|---|------|---------|
| 71 | Teacher with no assigned classes -> empty dashboard | Custom fixture |
| 72 | Class with no grades -> empty grid with student names only | teacherPage |
| 73 | Semester II with no data -> empty grid, not error | teacherPage |

## 5. Seed Data Changes Required

### 5.1 TOTP secrets for MFA-enabled roles

All admin, secretary, and teacher users need `totp_enabled = true` and a known `totp_secret`. The secret must be encrypted the same way the API stores it. If the API uses AES-GCM with `TOTP_ENCRYPTION_KEY`, the seed must store the ciphertext produced by that key.

**Alternative:** If encrypting in SQL is impractical, add a Go-based seed helper that calls the same encryption function and inserts the result.

### 5.2 Password hash for 2 students

Add `password_hash` (bcrypt of `catalog2026`) for:
- `andrei.moldovan@elev.rebreanu.ro` (Class 2A, primary)
- `alexandru.pop@elev.rebreanu.ro` (Class 6B, middle)

### 5.3 Activation token for 1 unactivated student

Add `activation_token = 'test-activation-token-radu'` for `radu.campean@elev.rebreanu.ro`, and ensure `activated_at IS NULL` and `password_hash IS NULL`.

### 5.4 data-testid attributes on pages

Some pages may need additional `data-testid` attributes for reliable selectors. The login page already has them. Other pages will need them added during implementation:
- Dashboard: class cards, admin cards, welcome message
- Catalog: semester buttons, subject tabs, grade grid rows, add/edit/delete buttons
- Grade input modal: all form fields
- Layout: sidebar items, hamburger button, user info
- Activation: all form steps
- SyncStatus: status indicator

## 6. Dependencies and Prerequisites

- **Backend running:** `make dev` must be running (API + DB + Redis + Nuxt)
- **Seed data loaded:** `make seed` with updated seed.sql
- **TOTP encryption key:** `TOTP_ENCRYPTION_KEY` env var must match between API and seed data
- **`otpauth` npm package:** Added as devDependency for TOTP code generation
- **Activation endpoint:** `GET /api/v1/auth/activate/{token}` and `POST /api/v1/auth/activate` must be implemented (or activation tests skipped initially)

## 7. Test Execution

```bash
# Local: start dev stack first, then run tests
make dev
cd web && npx playwright test

# CI: global-setup handles DB, CI config handles Nuxt
CI=true npx playwright test

# Run specific test file:
npx playwright test auth/login.spec.ts

# Run with UI mode for debugging:
npx playwright test --ui
```

## 8. Success Criteria

- All 73 tests pass against a fresh `make seed` database
- Every user role (admin, secretary, teacher, parent, student) has at least one login + dashboard test
- Grade CRUD tested for both primary (qualifiers) and middle (numeric) education levels
- Offline sync cycle tested: online -> offline -> mutate -> reconnect -> verify
- No test depends on another test's side effects (except within a single spec file)
- Tests run in under 5 minutes locally (sequential, single worker)

## 9. Test Count Summary

| Category | Tests |
|----------|-------|
| Auth and Login | 20 |
| Dashboard and Navigation | 17 |
| Catalog and Grading | 23 |
| Offline Sync | 6 |
| Error States | 4 |
| Edge Cases | 3 |
| **Total** | **73** |
