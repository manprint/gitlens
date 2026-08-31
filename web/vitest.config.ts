import { mergeConfig } from 'vitest/config'
import viteConfig from './vite.config.ts'

export default mergeConfig(viteConfig, {
  test: {
    environment: 'jsdom',
    environmentOptions: { jsdom: { url: 'http://localhost/' } },
    globals: false,
    setupFiles: ['./src/test/setup.ts'],
    css: false,
    restoreMocks: true,
    unstubEnvs: true,
    unstubGlobals: true,
    clearMocks: true,
    mockReset: true,
    fileParallelism: false,
    testTimeout: 5000,
    hookTimeout: 10000,
    env: { TZ: 'UTC' },
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx'],
    exclude: ['e2e/**'],
    reporters: process.env.CI ? ['default', 'junit'] : ['default'],
    outputFile: { junit: './coverage/junit.xml' },
    coverage: {
      provider: 'v8',
      reporter: ['text-summary', 'json-summary', 'lcov'],
      reportsDirectory: './coverage',
      all: true,
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/**/*.test.{ts,tsx}',
        'src/test/**',
        'src/components/ui/**',
        'src/api/generated.ts',
        'src/main.tsx',
        'src/vite-env.d.ts',
      ],
    },
  },
})
