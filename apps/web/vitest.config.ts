import { defineConfig } from 'vitest/config'

// Kept apart from vite.config.ts: that one stamps the version and proxies the
// API, neither of which a unit test wants.
export default defineConfig({
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
  },
})
