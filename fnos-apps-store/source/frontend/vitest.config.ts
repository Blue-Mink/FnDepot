import { defineConfig } from 'vitest/config';

/**
 * Unit tests only. Kept separate from vite.config.ts so the app build config
 * (notably `base: './'`, which the sub-path deployment depends on) stays
 * untouched, and so Vitest never tries to collect the Playwright e2e specs
 * under tests/e2e — those are driven by playwright.config.ts.
 */
export default defineConfig({
  test: {
    include: ['src/**/*.test.ts'],
    environment: 'node',
  },
});
