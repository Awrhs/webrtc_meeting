import { defineConfig, devices } from '@playwright/test'

/**
 * 跨浏览器 E2E 测试（Playwright matrix）。
 *
 * 默认只跑 smoke 套件（不依赖真实后端）；完整会议互通测试需要
 * 先启动 API + SFU + Postgres + Redis（make e2e-up），并设置
 * E2E_FULL=1。
 *
 *   npx playwright test --project=chromium
 */
export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.E2E_BASE_URL || 'http://localhost:3000',
    trace: 'on-first-retry',
  },
  // smoke 套件需要一个提供构建产物的服务器；完整互通测试需另外启动后端栈
  webServer: process.env.E2E_FULL
    ? undefined
    : {
        command: 'npm run preview -- --port 3000 --host 127.0.0.1',
        port: 3000,
        reuseExistingServer: !process.env.CI,
        timeout: 60_000,
      },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
    { name: 'webkit', use: { ...devices['Desktop Safari'] } },
  ],
})
