import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [vue()],
  test: {
    environment: 'jsdom',
    include: ['tests/unit/**/*.spec.ts'],
    // Playwright specs live under tests/e2e and run via `npm run test:e2e`.
    exclude: ['tests/e2e/**', 'node_modules/**'],
    // Process vuetify through Vite so its .css imports resolve in tests.
    server: { deps: { inline: ['vuetify'] } },
    coverage: {
      provider: 'v8',
      include: ['src/**'],
      exclude: ['src/main.ts', 'src/env.d.ts', 'src/**/*.vue'],
    },
  },
})
