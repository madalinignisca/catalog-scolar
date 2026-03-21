/**
 * In-memory mock implementation of the browser `Storage` interface.
 *
 * WHY THIS EXISTS
 * ───────────────
 * `lib/api.ts` reads and writes tokens via `localStorage.getItem / setItem /
 * removeItem`.  The happy-dom environment does provide a `localStorage`
 * implementation, but it persists across tests in the same worker process
 * unless manually cleared.  Our global `beforeEach` in `test/setup.ts` calls
 * `localStorage.clear()` to handle the common case, but sometimes a test needs
 * full control over a storage object — for example to inject a pre-existing
 * token, to test the exact sequence of reads/writes, or to swap localStorage
 * with sessionStorage.
 *
 * `createMockStorage()` gives you an isolated `Storage`-compatible object
 * backed by a plain `Map<string, string>` that you can pass around, inspect,
 * and reset independently of the global `localStorage`.
 *
 * USAGE
 * ─────
 *   import { createMockStorage } from '../helpers/mock-storage';
 *
 *   const storage = createMockStorage();
 *
 *   // Optionally override the global localStorage for the duration of a test:
 *   vi.stubGlobal('localStorage', storage);
 *
 *   // Or use it as a standalone object:
 *   storage.setItem('catalogro_access_token', 'my-jwt');
 *   expect(storage.getItem('catalogro_access_token')).toBe('my-jwt');
 *   expect(storage.length).toBe(1);
 *   storage.clear();
 *   expect(storage.length).toBe(0);
 */

/**
 * Creates and returns a new `Storage`-compatible object backed by an in-memory
 * `Map<string, string>`.
 *
 * The returned object implements the full `Storage` interface as defined by the
 * W3C spec and TypeScript's `lib.dom.d.ts`:
 *   - `getItem(key)`           → string | null
 *   - `setItem(key, value)`    → void
 *   - `removeItem(key)`        → void
 *   - `clear()`                → void
 *   - `key(index)`             → string | null
 *   - `length`                 → number  (getter, read-only)
 *
 * Each call to `createMockStorage()` returns a FRESH, independent instance —
 * there is no shared state between instances.
 */
export function createMockStorage(): Storage {
  /**
   * The underlying data store.  We use `Map` rather than a plain object so
   * that iteration order is guaranteed (insertion order) and so that keys
   * can never accidentally collide with built-in Object properties like
   * `toString` or `constructor`.
   */
  const store = new Map<string, string>();

  /**
   * The Storage object we will return.
   *
   * TypeScript's `Storage` interface requires an index signature
   * `[name: string]: any` for bracket-notation access (e.g. `storage['key']`).
   * We satisfy that with the `as Storage` cast below — the Map-based methods
   * are the canonical implementation; bracket access is not exercised by our
   * production code.
   */
  const mockStorage = {
    /**
     * `length` — the number of key/value pairs currently stored.
     *
     * Implemented as a getter so it reflects the current Map size on every
     * access rather than being a snapshot taken at construction time.
     *
     * Equivalent to: localStorage.length
     */
    get length(): number {
      return store.size;
    },

    /**
     * `getItem(key)` — retrieve the string value associated with `key`.
     *
     * Returns `null` (not `undefined`) if the key does not exist, matching
     * the real localStorage behaviour that calling code may depend on:
     *   `if (localStorage.getItem('token') === null) { ... }`
     *
     * @param key - The storage key to look up.
     * @returns   The stored string value, or `null` if not found.
     */
    getItem(key: string): string | null {
      // Map.get() returns `undefined` for missing keys; we convert to `null`
      // to match the Storage spec.
      return store.get(key) ?? null;
    },

    /**
     * `setItem(key, value)` — store a string value under the given key.
     *
     * If the key already exists its value is silently overwritten, exactly
     * like the real localStorage.
     *
     * Note: The real Storage coerces non-string values to strings via
     * `.toString()`. We don't implement that coercion here because TypeScript
     * strict mode forces callers to pass strings anyway.
     *
     * @param key   - The key to store the value under.
     * @param value - The string value to store.
     */
    setItem(key: string, value: string): void {
      store.set(key, value);
    },

    /**
     * `removeItem(key)` — delete the entry for `key`.
     *
     * If the key does not exist this is a no-op, matching the real
     * localStorage behaviour (no error is thrown).
     *
     * @param key - The key to remove.
     */
    removeItem(key: string): void {
      store.delete(key);
    },

    /**
     * `clear()` — remove ALL key/value pairs from the storage.
     *
     * After this call `length` will be 0 and every `getItem` call will
     * return `null`.
     */
    clear(): void {
      store.clear();
    },

    /**
     * `key(index)` — return the name of the nth key in insertion order.
     *
     * This method is rarely used in application code but is part of the
     * Storage interface.  Iterating via `for (let i = 0; i < s.length; i++)`
     * relies on it.
     *
     * Returns `null` if `index` is out of bounds (negative or >= length),
     * matching the real localStorage behaviour.
     *
     * @param index - Zero-based position in the insertion-order key list.
     * @returns     The key name at that position, or `null`.
     */
    key(index: number): string | null {
      // Convert the Map's keys to an array so we can index into them.
      // This is O(n) but fine for test usage where stores are small.
      const keys = Array.from(store.keys());
      // eslint-disable-next-line security/detect-object-injection -- index is a caller-supplied integer, bounds-checked on the same line
      return index >= 0 && index < keys.length ? (keys[index] ?? null) : null;
    },
  };

  // Cast to `Storage` to satisfy TypeScript's index-signature requirement on
  // the built-in Storage interface.  Our production code only uses the named
  // methods (getItem, setItem, etc.), never bracket notation, so this is safe.
  return mockStorage as Storage;
}
