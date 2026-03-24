/**
 * global-setup.ts
 *
 * Playwright global setup — runs once before the entire test suite.
 *
 * WHAT IT DOES
 * ────────────
 * 1. Resets the database to a known state (drop, create, migrate, seed)
 * 2. Waits for the API server to be healthy
 * 3. Waits for the Nuxt dev server to be ready
 *
 * WHY FRESH DB?
 * ─────────────
 * Tests create/modify data (grades, absences) freely. A fresh database
 * per run ensures no leftover state from previous runs causes failures.
 * The seed data provides known users, classes, and sample grades.
 *
 * PREREQUISITES
 * ─────────────
 * - Docker Compose must be running (database container)
 * - `make dev` should be running (API + Nuxt dev servers)
 * - The globalSetup only resets the DB — it does NOT start servers
 *
 * ESM NOTE
 * ────────
 * This file uses `import.meta.url` instead of `__dirname` because the
 * project has `"type": "module"` in package.json. `__dirname` is only
 * available in CommonJS modules, not in ESM.
 */

import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

/**
 * Derive the directory of THIS file from `import.meta.url`.
 * This is the ESM equivalent of `__dirname`.
 *
 * import.meta.url is the file:// URL of this module, e.g.:
 *   file:///home/.../web/test/e2e/global-setup.ts
 * fileURLToPath converts it to a filesystem path, then we take dirname.
 */
const __filename = fileURLToPath(import.meta.url);
// Walk three levels up: global-setup.ts → e2e/ → test/ → web/ → (monorepo root)
const PROJECT_ROOT = resolve(__filename, '..', '..', '..', '..');

/** Maximum time to wait for the API health endpoint to become reachable. */
const HEALTH_CHECK_TIMEOUT_MS = 30_000;

/** Maximum time to wait for the Nuxt dev server to become reachable. */
const NUXT_CHECK_TIMEOUT_MS = 60_000;

/**
 * waitForURL
 *
 * Polls a URL until it returns a 2xx status or the timeout is reached.
 * Used to wait for API and Nuxt servers to be ready before running tests.
 *
 * @param url - The URL to poll.
 * @param timeoutMs - Maximum wait time in milliseconds.
 * @throws Error if the URL does not become reachable within the timeout.
 */
async function waitForURL(url: string, timeoutMs: number): Promise<void> {
  const start = Date.now();
  // Poll every second — frequent enough to detect startup quickly without
  // hammering the server with hundreds of requests per second.
  const pollInterval = 1000;

  while (Date.now() - start < timeoutMs) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
    } catch {
      // Server not ready yet — keep polling.
      // fetch() throws on network errors (ECONNREFUSED), not HTTP errors.
    }
    await new Promise((resolve) => setTimeout(resolve, pollInterval));
  }

  throw new Error(`Timed out waiting for ${url} after ${String(timeoutMs)}ms`);
}

/**
 * runCommand
 *
 * Runs a binary synchronously from the project root.
 * Uses execFileSync (not exec/execSync) to avoid shell injection —
 * the binary and its arguments are passed as separate array elements.
 *
 * @param binary - The binary to execute (e.g., 'make').
 * @param args   - Arguments to pass to the binary.
 * @param env    - Optional environment variables; defaults to process.env.
 */
function runCommand(binary: string, args: string[], env?: NodeJS.ProcessEnv): void {
  console.log(`[global-setup] Running: ${binary} ${args.join(' ')}`);
  execFileSync(binary, args, {
    cwd: PROJECT_ROOT,
    stdio: 'inherit',
    timeout: 120_000,
    env: env ?? process.env,
  });
}

/**
 * globalSetup
 *
 * Playwright calls this function once before any test file runs.
 * It resets the database and waits for servers to be ready.
 */
async function globalSetup(): Promise<void> {
  console.log('[global-setup] Resetting database...');

  // Build a process environment that includes PGPASSWORD so that psql,
  // dropdb, and createdb can authenticate without an interactive prompt.
  // The password matches the Docker Compose postgres service config in
  // docker-compose.yml (user: catalogro, password: catalogro).
  const pgEnv: NodeJS.ProcessEnv = { ...process.env, PGPASSWORD: 'catalogro' };

  // Step 1: Terminate any active connections to the 'catalogro' database.
  // Without this, dropdb will fail with "database is being accessed by other
  // users" if the API server or any other client holds an open connection.
  try {
    execFileSync(
      'psql',
      [
        '-U',
        'catalogro',
        '-h',
        'localhost',
        '-d',
        'postgres',
        '-c',
        "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'catalogro' AND pid <> pg_backend_pid();",
      ],
      { cwd: PROJECT_ROOT, stdio: 'inherit', timeout: 10_000, env: pgEnv },
    );
  } catch {
    // Ignore errors here — the database may not exist yet on the first run.
    // If it does not exist, the next dropdb --if-exists will also be a no-op.
  }

  // Step 2: Drop the database completely (if it exists) for a clean slate.
  // --if-exists makes the command a no-op rather than an error when absent.
  execFileSync('dropdb', ['-U', 'catalogro', '-h', 'localhost', '--if-exists', 'catalogro'], {
    cwd: PROJECT_ROOT,
    stdio: 'inherit',
    timeout: 30_000,
    env: pgEnv,
  });

  // Step 3: Recreate the empty database.
  execFileSync('createdb', ['-U', 'catalogro', '-h', 'localhost', 'catalogro'], {
    cwd: PROJECT_ROOT,
    stdio: 'inherit',
    timeout: 30_000,
    env: pgEnv,
  });

  // Step 4: Run all goose migrations to create the full schema.
  // `make migrate` calls: goose -dir api/db/migrations postgres $DATABASE_URL up
  runCommand('make', ['migrate']);

  // Step 5: Load seed data — test users, classes, grades, TOTP secrets, etc.
  // `make seed` calls: psql $DATABASE_URL -f api/db/seed.sql
  runCommand('make', ['seed']);

  console.log('[global-setup] Database reset complete. Waiting for servers...');

  // Step 6: Wait for the Go API to respond to health checks.
  // The health endpoint returns 200 OK when the server and DB are both ready.
  await waitForURL('http://localhost:8080/healthz', HEALTH_CHECK_TIMEOUT_MS);
  console.log('[global-setup] API server is ready.');

  // Step 7: Wait for the Nuxt SSR dev server to be reachable.
  // Nuxt takes longer than the API (it compiles Vue on first request).
  await waitForURL('http://localhost:3000', NUXT_CHECK_TIMEOUT_MS);
  console.log('[global-setup] Nuxt dev server is ready.');

  console.log('[global-setup] Setup complete. Starting tests...');
}

export default globalSetup;
