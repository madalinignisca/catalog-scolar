# Linting, Code Quality & Security Checks — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Set up comprehensive linting, code quality, and security scanning for the CatalogRO monorepo, enforced via pre-commit hooks.

**Architecture:** Config-file-driven approach — each tool gets its own config file at the repo or package root. A `.pre-commit-config.yaml` orchestrates all hooks in a specific order (fast to slow, fail-fast). The Makefile exposes `check`, `security`, `fix`, and `hooks-install` targets for running checks outside of git hooks.

**Tech Stack:** golangci-lint v2, ESLint v9 (flat config), Prettier, semgrep, gitleaks, hadolint, govulncheck, pre-commit framework, editorconfig-checker, helm lint

**Spec:** `docs/superpowers/specs/2026-03-21-linting-quality-security-design.md`

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `api/.golangci.yml` | Create | Go linter configuration (v2 format) |
| `.editorconfig` | Create | Cross-editor encoding, indent, whitespace rules |
| `.gitleaks.toml` | Create | Gitleaks allowlist for dev credentials |
| `web/.prettierignore` | Create | Prettier file exclusions |
| `.semgrep.yml` | Create | Custom Go security rules (7 rules) |
| `.pre-commit-config.yaml` | Create | Pre-commit hook orchestration (10 hooks) |
| `web/eslint.config.mjs` | Modify | Add security, a11y, import plugins + strict rules + typed config |
| `web/package.json` | Modify | Add ESLint plugin devDependencies |
| `Makefile` | Modify | Add check, security, fix, hooks-install targets |

---

### Task 1: Install System-Level Dependencies

**Files:** None (system tools only)

- [ ] **Step 1: Install pre-commit**

```bash
uv tool install pre-commit
```

Expected: `pre-commit --version` outputs a version number.

- [ ] **Step 2: Install semgrep**

```bash
uv tool install semgrep
```

Expected: `semgrep --version` outputs a version number.

- [ ] **Step 3: Install govulncheck and goimports**

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest && go install golang.org/x/tools/cmd/goimports@latest
```

Expected: `which govulncheck goimports` both return paths.

- [ ] **Step 4: Install hadolint**

```bash
# Download binary for Linux amd64
sudo curl -L https://github.com/hadolint/hadolint/releases/latest/download/hadolint-Linux-x86_64 -o /usr/local/bin/hadolint && sudo chmod +x /usr/local/bin/hadolint
```

Expected: `hadolint --version` outputs a version number.

- [ ] **Step 5: Verify all tools**

```bash
pre-commit --version && semgrep --version && govulncheck -version && goimports -h 2>&1 | head -1 && hadolint --version && golangci-lint --version && helm version --short
```

Expected: All commands succeed, no "not found" errors.

---

### Task 2: Create `.editorconfig`

**Files:**
- Create: `.editorconfig`

- [ ] **Step 1: Create the file**

```ini
root = true

[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
trim_trailing_whitespace = true
indent_style = space
indent_size = 2

[*.go]
indent_style = tab

[Makefile]
indent_style = tab

[*.sql]
indent_size = 4

[*.md]
trim_trailing_whitespace = false
```

- [ ] **Step 2: Verify with editorconfig-checker**

```bash
npx editorconfig-checker -exclude '.git|node_modules|.nuxt|.output|bin'
```

Expected: Either clean output or a list of existing violations to note (not a blocker — editorconfig-checker will enforce going forward).

- [ ] **Step 3: Commit**

```bash
git add .editorconfig
git commit -m "chore: add .editorconfig for cross-editor consistency"
```

---

### Task 3: Create `.gitleaks.toml`

**Files:**
- Create: `.gitleaks.toml`

- [ ] **Step 1: Create the file**

The allowlist must cover:
- `Makefile` line 5: `postgres://catalogro:catalogro@localhost:5432/...`
- `CLAUDE.md` environment variable examples with placeholder values
- `api/db/seed.sql` stable test UUIDs
- `docker-compose.yml` service passwords

```toml
[allowlist]
description = "CatalogRO global allowlist"
paths = [
  '''api/db/seed\.sql''',
  '''docs/''',
]
regexTarget = "match"
regexes = [
  # Localhost dev database URLs (not real credentials)
  '''postgres://catalogro:catalogro@localhost''',
  # Docker compose / Makefile dev defaults
  '''POSTGRES_PASSWORD.*catalogro''',
  # MinIO dev defaults
  '''minioadmin''',
]
```

- [ ] **Step 2: Verify gitleaks runs clean**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro && gitleaks detect --source . --config .gitleaks.toml --no-git -v 2>&1 | tail -5
```

Expected: `no leaks found` or only findings that need additional allowlist tuning.

- [ ] **Step 3: If gitleaks reports false positives, add them to the allowlist and re-run**

- [ ] **Step 4: Commit**

```bash
git add .gitleaks.toml
git commit -m "chore: add gitleaks config with dev credential allowlist"
```

---

### Task 4: Create `web/.prettierignore`

**Files:**
- Create: `web/.prettierignore`

- [ ] **Step 1: Create the file**

```
.nuxt/
.output/
node_modules/
dist/
*.min.js
```

- [ ] **Step 2: Verify prettier still works**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx prettier --check .
```

Expected: Either all files formatted or a list of formatting issues. No errors about scanning `.nuxt/` or `node_modules/`.

- [ ] **Step 3: Commit**

```bash
git add web/.prettierignore
git commit -m "chore: add .prettierignore to skip generated files"
```

---

### Task 5: Create `api/.golangci.yml` (v2 Format)

**Files:**
- Create: `api/.golangci.yml`

**Important:** golangci-lint v2.11.1 is installed. The config format is v2 — `gosimple` is merged into `staticcheck`, settings are nested under `linters.settings`, exclusions under `linters.exclusions`.

- [ ] **Step 1: Create the config file**

```yaml
version: "2"

run:
  timeout: 5m

linters:
  default: none
  enable:
    # Bug catchers
    - govet
    - staticcheck    # includes former gosimple + stylecheck
    - errcheck
    - nilerr
    - bodyclose
    - sqlclosecheck
    - exhaustive
    # Security
    - gosec
    # Code quality
    - revive
    - gocritic
    - ineffassign
    - unused

  settings:
    exhaustive:
      default-signifies-exhaustive: true
    errcheck:
      check-type-assertions: true
      check-blank: false
    gocritic:
      enabled-tags:
        - diagnostic
        - style
        - performance
    revive:
      rules:
        - name: exported
        - name: var-naming
        - name: indent-error-flow
        - name: error-return
        - name: error-naming
        - name: unexported-return
        - name: blank-imports

  exclusions:
    generated: strict
    presets:
      - comments
      - common-false-positives
      - std-error-handling
    paths:
      - db/generated/
    rules:
      - path: '_test\.go'
        linters:
          - gosec
        text: "G104"
```

- [ ] **Step 2: Verify config is valid**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/api && golangci-lint config verify
```

Expected: Exit code 0 with no errors.

- [ ] **Step 3: Run golangci-lint against current code**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/api && golangci-lint run ./...
```

Expected: Either clean or a list of existing violations. Note any violations that need fixing — they should be fixed before the pre-commit hook is enabled, otherwise every commit will fail.

- [ ] **Step 4: Fix any lint violations found in existing Go code**

Address each violation. Common ones will be:
- `errcheck`: unchecked error returns (add `if err != nil` checks or explicit `_ =` for intentional ignores)
- `revive`: naming issues
- `gosec`: potential security issues

- [ ] **Step 5: Commit**

```bash
git add api/.golangci.yml
git add -u api/  # any Go file fixes
git commit -m "chore: add golangci-lint v2 config with strict linter set"
```

---

### Task 6: Update ESLint — Install Dependencies

**Files:**
- Modify: `web/package.json`

- [ ] **Step 1: Install new ESLint plugins**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npm install --save-dev @eslint/js eslint-plugin-security eslint-plugin-vuejs-accessibility eslint-plugin-import-x
```

Expected: `package.json` devDependencies updated, `package-lock.json` updated, no install errors.

- [ ] **Step 2: Verify packages installed**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && ls node_modules/eslint-plugin-security node_modules/eslint-plugin-vuejs-accessibility node_modules/eslint-plugin-import-x node_modules/@eslint/js -d
```

Expected: All four directories exist.

- [ ] **Step 3: Commit dependency changes**

```bash
git add web/package.json web/package-lock.json
git commit -m "chore: add ESLint security, a11y, and import plugins"
```

---

### Task 7: Update ESLint — Rewrite Config

**Files:**
- Modify: `web/eslint.config.mjs`

This is the most complex modification. The current config needs:
1. New plugin imports
2. Typed linting setup (`parserOptions.projectService: true`)
3. New rules added
4. Plugin configs spread in correct order

**Note:** This upgrades from `tseslint.configs.strict` to `tseslint.configs.strictTypeChecked`. This is required for `no-floating-promises` and `strict-boolean-expressions` (which need type information), but it also enables additional type-checked rules. Expect more violations than just the new rules listed below — address them all in Step 4.

- [ ] **Step 1: Rewrite `web/eslint.config.mjs`**

Replace the entire file with:

```javascript
import eslint from '@eslint/js';
import tseslint from 'typescript-eslint';
import vue from 'eslint-plugin-vue';
import security from 'eslint-plugin-security';
import vuejsAccessibility from 'eslint-plugin-vuejs-accessibility';
import importX from 'eslint-plugin-import-x';

export default tseslint.config(
  // Base configs
  eslint.configs.recommended,
  ...tseslint.configs.strictTypeChecked,
  ...vue.configs['flat/recommended'],
  ...vuejsAccessibility.configs['flat/recommended'],
  security.configs.recommended,

  // TypeScript typed linting — requires .nuxt/tsconfig.json (run npm install first)
  {
    languageOptions: {
      parserOptions: {
        projectService: true,
      },
    },
  },

  // Vue SFC parser config
  {
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser,
        projectService: true,
      },
    },
  },

  // Import plugin config
  {
    plugins: {
      'import-x': importX,
    },
    rules: {
      'import-x/no-unresolved': 'off', // TypeScript handles this
      'import-x/no-duplicates': 'error',
      'import-x/no-self-import': 'error',
      'import-x/no-cycle': ['error', { maxDepth: 3 }],
      'import-x/order': [
        'error',
        {
          groups: ['builtin', 'external', 'internal', 'parent', 'sibling', 'index'],
          'newlines-between': 'always',
        },
      ],
    },
  },

  // Project rules
  {
    rules: {
      // Existing rules (preserved)
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      '@typescript-eslint/no-explicit-any': 'error',
      'vue/multi-word-component-names': 'off',

      // New strict rules
      '@typescript-eslint/no-floating-promises': 'error',
      '@typescript-eslint/strict-boolean-expressions': [
        'error',
        {
          allowNullableObject: true,
          allowNullableBoolean: true,
          allowNumber: true,
          allowNullableString: false,
        },
      ],
      'vue/no-v-html': 'error',
    },
  },

  // Ignores
  {
    ignores: ['.nuxt/', '.output/', 'node_modules/', 'dist/'],
  },
);
```

- [ ] **Step 2: Verify ESLint config loads without errors**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx eslint --print-config pages/index.vue > /dev/null
```

Expected: Exit code 0. If it errors with "Cannot find tsconfig", run `npm run postinstall` first (generates `.nuxt/tsconfig.json`).

- [ ] **Step 3: Run ESLint against current code**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx eslint .
```

Expected: A list of new violations from the stricter rules. Note them — they must be fixed before the pre-commit hook is enabled.

- [ ] **Step 4: Fix ESLint violations in existing web code**

Common violations to expect:
- `@typescript-eslint/no-floating-promises` — add `await` or `void` to unhandled promises
- `@typescript-eslint/strict-boolean-expressions` — replace `if (str)` with `if (str !== undefined)` for nullable strings
- `vue/no-v-html` — replace `v-html` with text interpolation or a sanitized rendering approach
- `import-x/order` — reorder imports (auto-fixable with `--fix`)
- `vuejs-accessibility/*` — add missing alt attrs, aria labels

Auto-fix what's possible first:

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx eslint --fix .
```

Then manually fix remaining violations.

- [ ] **Step 5: Verify clean lint**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro/web && npx eslint . && npx prettier --check .
```

Expected: Exit code 0 for both.

- [ ] **Step 6: Commit**

```bash
git add web/eslint.config.mjs
git add -u web/  # any source fixes
git commit -m "chore: add security, a11y, import plugins and strict rules to ESLint"
```

---

### Task 8: Create `.semgrep.yml`

**Files:**
- Create: `.semgrep.yml`

- [ ] **Step 1: Create the custom rules file**

```yaml
rules:
  - id: sql-string-concat
    patterns:
      - pattern-either:
          - pattern: |
              fmt.Sprintf("... SELECT ...", ...)
          - pattern: |
              fmt.Sprintf("... INSERT ...", ...)
          - pattern: |
              fmt.Sprintf("... UPDATE ...", ...)
          - pattern: |
              fmt.Sprintf("... DELETE ...", ...)
          - pattern: |
              fmt.Sprintf("... FROM ...", ...)
          - pattern: |
              $X + "... SELECT ..."
          - pattern: |
              $X + "... INSERT ..."
          - pattern: |
              $X + "... UPDATE ..."
          - pattern: |
              $X + "... DELETE ..."
          - pattern: |
              "... SELECT ..." + $X
          - pattern: |
              "... INSERT ..." + $X
          - pattern: |
              "... UPDATE ..." + $X
          - pattern: |
              "... DELETE ..." + $X
    message: >
      SQL query built with string concatenation or fmt.Sprintf. Use parameterized
      queries (sqlc or pgx query parameters) to prevent SQL injection.
    languages: [go]
    severity: ERROR
    paths:
      include:
        - api/
      exclude:
        - "*_test.go"

  - id: hardcoded-school-id
    pattern: |
      $VAR = "........-....-....-....-............"
    message: >
      Hardcoded UUID that may be a school_id. Use configuration or context-based
      tenant resolution instead.
    languages: [go]
    severity: WARNING
    paths:
      include:
        - api/
      exclude:
        - "*_test.go"
        - "api/db/seed.sql"

  - id: jwt-secret-in-code
    patterns:
      - pattern-either:
          - pattern: |
              $SECRET = "..."
          - pattern: |
              $SECRET := "..."
      - metavariable-regex:
          metavariable: $SECRET
          regex: (?i)(jwt.?secret|totp.?key|totp.?secret|encryption.?key|signing.?key)
    message: >
      Secret value appears to be hardcoded. Load secrets from environment
      variables or a secrets manager.
    languages: [go]
    severity: ERROR
    paths:
      include:
        - api/
      exclude:
        - "*_test.go"
        - "api/internal/config/*"

  - id: insecure-crypto
    pattern-either:
      - pattern: |
          import "crypto/md5"
      - pattern: |
          import "crypto/sha1"
      - pattern: |
          import "crypto/des"
    message: >
      Weak cryptographic primitive. Use crypto/sha256, crypto/sha512, or
      crypto/aes for GDPR-compliant data handling.
    languages: [go]
    severity: ERROR
    paths:
      include:
        - api/

  - id: missing-rows-err-check
    patterns:
      - pattern: |
          $ROWS, $ERR := $DB.Query(...)
          ...
          for $ROWS.Next() {
            ...
          }
      - pattern-not: |
          $ROWS, $ERR := $DB.Query(...)
          ...
          for $ROWS.Next() {
            ...
          }
          ...
          $ROWS.Err()
    message: >
      rows.Err() not checked after iteration. Database errors during iteration
      are only reported via Err(). This can cause silent data loss.
    languages: [go]
    severity: WARNING
    paths:
      include:
        - api/

  - id: rls-missing-tenant-context
    patterns:
      - pattern-either:
          - pattern: |
              $POOL.Query($CTX, ...)
          - pattern: |
              $POOL.Exec($CTX, ...)
          - pattern: |
              $POOL.QueryRow($CTX, ...)
      - pattern-not-inside: |
          func(...) {
            ...
            SetTenant(...)
            ...
          }
    message: >
      Database query without SetTenant() in the same function. Ensure RLS
      tenant context is set (may be handled by middleware — add // nosemgrep
      if so).
    languages: [go]
    severity: WARNING
    paths:
      include:
        - api/
      exclude:
        - "*_test.go"
        - "api/internal/platform/*"

  - id: unvalidated-input-to-db
    pattern-either:
      - patterns:
          - pattern: |
              func $HANDLER($W http.ResponseWriter, $R *http.Request) {
                ...
                $BODY := $R.Body
                ...
                $DB.$METHOD($CTX, ..., $BODY, ...)
              }
      - patterns:
          - pattern: |
              func $HANDLER($W http.ResponseWriter, $R *http.Request) {
                ...
                $PARAM := $R.URL.Query().Get(...)
                ...
                $DB.$METHOD($CTX, ..., $PARAM, ...)
              }
    message: >
      HTTP request data flows to database query without visible validation.
      Validate and sanitize input before passing to queries. (Best-effort:
      intra-function only — add // nosemgrep if validation happens in a
      called function.)
    languages: [go]
    severity: WARNING
    paths:
      include:
        - api/
      exclude:
        - "*_test.go"
```

- [ ] **Step 2: Verify semgrep config is valid**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro && semgrep --validate --config .semgrep.yml
```

Expected: `Configuration is valid` or similar success message.

- [ ] **Step 3: Run semgrep against current code**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro && semgrep --config .semgrep.yml api/
```

Expected: Either clean or a list of findings. The current codebase has minimal handler logic (all 501 stubs), so there should be few or no findings.

- [ ] **Step 4: Commit**

```bash
git add .semgrep.yml
git commit -m "chore: add semgrep custom rules for SQL injection, RLS, and crypto"
```

---

### Task 9: Update Makefile

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Update .PHONY declaration**

Replace the existing `.PHONY` line (lines 1-2) with:

```makefile
.PHONY: dev dev-api dev-web test test-api test-web lint lint-api lint-web \
       migrate migrate-down migrate-status sqlc seed build clean help \
       check security fix hooks-install
```

- [ ] **Step 2: Add new targets between the Linting section and Build section (insert after line 64, before the `# ── Build` comment)**

```makefile
# ── Quality & Security ─────────────────────────────────────
check: ## Run all quality checks (same as pre-commit hooks)
	gitleaks detect --source . --config .gitleaks.toml --no-git -v
	npx editorconfig-checker -exclude '.git|node_modules|.nuxt|.output|bin'
	hadolint api/Dockerfile web/Dockerfile
	cd web && npx prettier --check .
	cd web && npx eslint .
	cd api && golangci-lint run ./...
	cd api && govulncheck ./...
	cd web && npm audit --audit-level=high
	helm lint helm/catalogro
	semgrep --config .semgrep.yml api/

security: ## Run security-focused checks only
	cd api && golangci-lint run --enable-only gosec ./...
	cd api && govulncheck ./...
	cd web && npm audit --audit-level=high
	gitleaks detect --source . --config .gitleaks.toml --no-git -v
	semgrep --config .semgrep.yml api/

fix: ## Auto-fix formatting and lint issues
	cd web && npx prettier --write .
	cd web && npx eslint --fix .
	cd api && find . -name '*.go' -not -path './db/generated/*' | xargs goimports -w

hooks-install: ## Install pre-commit hooks (run once after clone)
	pre-commit install
	@echo "Pre-commit hooks installed. Run 'npm install' in web/ if not done."
	@echo "Run 'go mod tidy' in api/ if go.sum is missing."
```

- [ ] **Step 3: Verify new targets work**

```bash
make help | grep -E '(check|security|fix|hooks-install)'
```

Expected: All four targets listed with descriptions.

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "chore: add check, security, fix, hooks-install Makefile targets"
```

---

### Task 10: Create `.pre-commit-config.yaml`

**Files:**
- Create: `.pre-commit-config.yaml`

This is the orchestration file. All 10 hooks in the designed execution order.

- [ ] **Step 1: Create the config file**

```yaml
repos:
  # 1. Secret detection (fastest, fail-first)
  - repo: https://github.com/gitleaks/gitleaks
    rev: v8.24.0
    hooks:
      - id: gitleaks
        args: ['--config', '.gitleaks.toml']

  # 2. EditorConfig compliance
  - repo: https://github.com/editorconfig-checker/editorconfig-checker
    rev: v3.2.0
    hooks:
      - id: editorconfig-checker
        exclude: '(\.git/|node_modules/|\.nuxt/|\.output/|bin/|\.png$|\.ico$|\.woff2?$)'

  # 3. Dockerfile linting (uses local binary installed in Task 1)
  - repo: local
    hooks:
      - id: hadolint
        name: hadolint
        entry: hadolint
        language: system
        types: [dockerfile]

  # 4-10. Local hooks (project-specific tools)
  - repo: local
    hooks:
      # 4. Prettier (formatting before logic checks)
      - id: prettier
        name: prettier
        entry: bash -c 'cd web && npx prettier --check --ignore-unknown'
        language: system
        files: '^web/.*\.(ts|vue|json|css)$'
        pass_filenames: false

      # 5. ESLint
      - id: eslint
        name: eslint
        entry: bash -c 'cd web && npx eslint .'
        language: system
        files: '^web/.*\.(ts|vue)$'
        pass_filenames: false

      # 6. golangci-lint (needs full module context)
      - id: golangci-lint
        name: golangci-lint
        entry: bash -c 'cd api && golangci-lint run ./...'
        language: system
        files: '^api/.*\.go$'
        pass_filenames: false

      # 7. Go vulnerability check (on go.mod or go.sum changes — broader than spec's
      #    go.sum-only trigger, intentionally catching new dependency additions too)
      - id: govulncheck
        name: govulncheck
        entry: bash -c 'cd api && govulncheck ./...'
        language: system
        files: '^api/go\.(mod|sum)$'
        pass_filenames: false

      # 8. npm audit (only on lockfile changes)
      - id: npm-audit
        name: npm-audit
        entry: bash -c 'cd web && npm audit --audit-level=high'
        language: system
        files: '^web/package-lock\.json$'
        pass_filenames: false

      # 9. Helm lint
      - id: helm-lint
        name: helm-lint
        entry: helm lint helm/catalogro
        language: system
        files: '^helm/'
        pass_filenames: false

      # 10. Semgrep custom security rules
      - id: semgrep
        name: semgrep
        entry: semgrep --config .semgrep.yml --error api/
        language: system
        files: '^api/.*\.go$'
        pass_filenames: false
```

- [ ] **Step 2: Pin hook versions to latest available**

Check the latest release tags for each repo hook and update the `rev` values:

```bash
# Check latest releases (adjust if different)
echo "gitleaks: check https://github.com/gitleaks/gitleaks/releases"
echo "editorconfig-checker: check https://github.com/editorconfig-checker/editorconfig-checker/releases"
echo "hadolint: check https://github.com/hadolint/hadolint/releases"
```

Update `rev` values in the file to match latest stable releases.

- [ ] **Step 3: Install the hooks**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro && pre-commit install
```

Expected: `pre-commit installed at .git/hooks/pre-commit`

- [ ] **Step 4: Test hooks run successfully**

```bash
cd /home/gabriel/openpublic/catalog-scolar/catalogro && pre-commit run --all-files
```

Expected: All 10 hooks pass (or skip for hooks scoped to files that don't exist in the staged set). If any fail, fix the underlying issue — do NOT disable the hook.

- [ ] **Step 5: Fix any failures from the full run**

Common issues:
- editorconfig-checker may flag trailing whitespace in existing files
- hadolint may flag Dockerfile issues (unpinned base images, missing `--no-cache` on apt)
- helm lint may flag chart issues

Fix each and verify the hook passes.

- [ ] **Step 6: Commit**

```bash
git add .pre-commit-config.yaml
git add -u  # any files fixed from hook failures
git commit -m "chore: add pre-commit hooks for quality, security, and formatting"
```

---

### Task 11: Verify End-to-End

**Files:** None (verification only)

- [ ] **Step 1: Run full check suite via Makefile**

```bash
make check
```

Expected: All checks pass end-to-end (gitleaks, editorconfig, hadolint, prettier, eslint, golangci-lint, govulncheck, npm audit, helm lint, semgrep).

- [ ] **Step 2: Test pre-commit hooks with a dummy change**

```bash
# Make a trivial change to a Go file
echo "" >> api/cmd/server/main.go
git add api/cmd/server/main.go
git commit -m "test: verify pre-commit hooks"
# If hooks pass, the commit succeeds. Reset afterward:
git reset HEAD~1
git checkout -- api/cmd/server/main.go
```

Expected: Hooks fire for Go-scoped hooks (golangci-lint, semgrep). Hooks scoped to web/ should be skipped. Commit succeeds or fails with clear error.

- [ ] **Step 3: Test pre-commit hooks with a web change**

```bash
echo "" >> web/lib/api.ts
git add web/lib/api.ts
git commit -m "test: verify pre-commit hooks for web"
git reset HEAD~1
git checkout -- web/lib/api.ts
```

Expected: Hooks fire for web-scoped hooks (prettier, eslint). Go hooks should be skipped.

- [ ] **Step 4: Run make security**

```bash
make security
```

Expected: All security checks pass.

- [ ] **Step 5: Run make fix (dry check)**

```bash
make fix
```

Expected: Auto-formatters run without errors. Any file changes are formatting-only (verify with `git diff`).
