/**
 * Global test setup file for CatalogRO Vitest suite.
 *
 * This file is executed ONCE per worker before any test suite starts.
 * It is referenced in vitest.config.ts under `test.setupFiles`.
 *
 * Because `vitest.config.ts` sets `globals: true`, Vitest injects `vi`,
 * `describe`, `it`, `expect`, `beforeEach`, etc. into the global scope at
 * runtime.  The triple-slash reference below makes TypeScript aware of those
 * globals at compile time so the type-checker does not complain about `vi`
 * being an unknown identifier.
 *
 * Responsibilities:
 *  1. Stub Nuxt auto-import globals that the source code calls at runtime but
 *     that don't exist in the happy-dom environment (no Nuxt runtime here).
 *  2. Forward core Vue reactivity primitives so that any composable that calls
 *     `ref()` / `computed()` / `readonly()` without an explicit import works.
 *  3. Register a beforeEach hook that resets all mocks and clears localStorage
 *     before every single test, so tests can never leak state into each other.
 *
 * HOW `vi.stubGlobal` WORKS
 * ─────────────────────────
 * `vi.stubGlobal(name, value)` writes `value` onto the global `window` object
 * (in happy-dom that IS the global scope) under the key `name`.  Combined with
 * `vi.clearAllMocks()` in beforeEach, these stubs are automatically tracked
 * and can be reset between tests.
 *
 * We call stubGlobal here (in setup.ts) rather than inside each test file so
 * that every test file gets the stubs for free — no boilerplate required.
 */

/// <reference types="vitest/globals" />

import { ref, computed, readonly } from 'vue';

// ── 1. Nuxt router helper ────────────────────────────────────────────────────

/**
 * `navigateTo` is Nuxt's programmatic navigation function.
 * In tests we don't want actual navigation to occur (there is no Nuxt router
 * running), so we replace it with a plain spy function. Tests can then assert
 * that navigation was (or was not) triggered:
 *
 *   expect(navigateTo).toHaveBeenCalledWith('/login');
 */
vi.stubGlobal('navigateTo', vi.fn());

// ── 2. Nuxt runtime config ───────────────────────────────────────────────────

/**
 * `useRuntimeConfig()` is called inside `lib/api.ts` → `getApiBase()` whenever
 * `import.meta.client` is true (which it is in our test environment — see
 * vitest.config.ts `define`).
 *
 * We return a minimal config object that matches the shape the code actually
 * accesses:  `useRuntimeConfig().public.apiBase`
 *
 * Using localhost:8080 ensures that:
 *  - URL construction in api() produces a predictable base URL in tests.
 *  - Mock fetch handlers can match against full URLs if needed.
 */
vi.stubGlobal('useRuntimeConfig', () => ({
  public: {
    apiBase: 'http://localhost:8080/api/v1',
  },
}));

// ── 3. Vue reactivity primitives ─────────────────────────────────────────────

/**
 * Nuxt auto-imports `ref`, `computed`, and `readonly` from Vue so that
 * composables can use them without an explicit `import { ref } from 'vue'`.
 * In the Vitest environment those auto-imports don't exist, so we wire them up
 * manually as globals here.
 *
 * We import the real Vue implementations (not mocks) because we want reactive
 * behaviour to work correctly in composable tests — only the *import mechanism*
 * is being patched, not the functionality.
 */
vi.stubGlobal('ref', ref);
vi.stubGlobal('computed', computed);
vi.stubGlobal('readonly', readonly);

// ── 4. Per-test cleanup hook ─────────────────────────────────────────────────

/**
 * Before EVERY individual test (`it(...)` / `test(...)`):
 *
 *  a) `vi.clearAllMocks()` — resets the call history (.mock.calls,
 *     .mock.results) of every mock/spy created with vi.fn() or vi.spyOn().
 *     This does NOT remove the mock implementation; it only wipes recorded
 *     calls so that previous tests don't bleed into assertions in later ones.
 *
 *  b) `localStorage.clear()` — removes every key/value pair from the
 *     happy-dom localStorage. Our auth code stores tokens there
 *     (`catalogro_access_token`, `catalogro_refresh_token`). Without this
 *     cleanup a test that sets a token would affect all subsequent tests.
 */
beforeEach(() => {
  // Reset all mock call histories without removing mock implementations.
  vi.clearAllMocks();

  // Wipe any tokens or other state that lib/api.ts stored in localStorage.
  localStorage.clear();
});
