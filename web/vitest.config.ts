/**
 * Vitest configuration for the CatalogRO web application.
 *
 * We use `defineVitestConfig` from `@nuxt/test-utils` instead of plain
 * `defineConfig` from vitest because it integrates the Nuxt module resolution
 * layer — this means auto-imported composables (useRoute, useState, etc.) and
 * Nuxt aliases (#imports, ~/) resolve correctly during tests, exactly as they
 * would in the real app.
 */
import { defineVitestConfig } from '@nuxt/test-utils/config';

export default defineVitestConfig({
  test: {
    /**
     * environment: 'happy-dom'
     *
     * Tells Vitest to simulate a browser-like DOM environment for every test
     * file. `happy-dom` is a lightweight alternative to jsdom — it implements
     * the browser APIs (window, document, localStorage, fetch…) that our Vue
     * components and composables rely on, but runs much faster than a real
     * browser. The alternative would be 'jsdom' (heavier) or 'node' (no DOM at
     * all, which would break anything that reads window/document).
     */
    environment: 'happy-dom',

    /**
     * globals: true
     *
     * Makes Vitest's test helpers — describe, it, test, expect, vi, beforeEach,
     * afterEach, etc. — available in every test file WITHOUT an explicit
     * `import { describe, it, expect } from 'vitest'` at the top. This mirrors
     * how Jest behaves and keeps test files less noisy.
     *
     * TypeScript note: to get type-checking for the globals, add
     * `"types": ["vitest/globals"]` to your tsconfig.json (or the test
     * tsconfig). Without that you will see "describe is not defined" TS errors
     * even though the tests run fine at runtime.
     */
    globals: true,

    /**
     * include: ['test/**\/*.test.ts']
     *
     * Restricts test discovery to files inside the `web/test/` folder that end
     * with `.test.ts`. The double-star glob (`**`) means it recurses into any
     * depth of subdirectories (e.g. test/helpers/, test/composables/, etc.).
     *
     * Why restrict? By default Vitest would scan ALL .test.ts files in the
     * project, including inside node_modules or generated .nuxt/ files, which
     * can cause false failures or extremely slow startup. Explicit inclusion is
     * safer in a monorepo.
     */
    include: ['test/**/*.test.ts'],

    /**
     * setupFiles: ['test/setup.ts']
     *
     * A list of modules to execute ONCE before any test suite runs (but after
     * the test environment is initialised). We use this file to:
     *   - stub Nuxt auto-imports that aren't available in the test environment
     *     (navigateTo, useRuntimeConfig, ref, computed, …)
     *   - register global beforeEach hooks that reset mocks and localStorage
     *
     * Order matters if you list multiple setup files — they run sequentially.
     */
    setupFiles: ['test/setup.ts'],
  },

  /**
   * define: { 'import.meta.client': true }
   *
   * Vite's `define` option performs compile-time constant replacement — every
   * occurrence of the string `import.meta.client` in source files is replaced
   * with the literal `true` before the test runs.
   *
   * Why is this necessary?  In `lib/api.ts` (and elsewhere) we guard browser-
   * only code with `if (import.meta.client) { ... }`. In production, Nuxt sets
   * this to `true` on the client bundle and `false` on the SSR bundle.  During
   * Vitest tests there is no Nuxt build pipeline, so the value would be
   * `undefined` (falsy), causing localStorage calls and token handling to be
   * silently skipped, making tests impossible to write properly.
   *
   * Setting it to `true` tells the tested code "we are in a browser context",
   * which matches the happy-dom environment we configured above.
   */
  define: {
    'import.meta.client': true,
  },
});
