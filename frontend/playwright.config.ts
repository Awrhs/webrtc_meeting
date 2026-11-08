import { defineConfig, devices } from '@playwright/test'

/**
 * 跨浏览器 E2E 测试（Playwright matrix）。
 *
 * 两个套件：
 *  - smoke：不依赖后端，三个浏览器都跑（应用外壳可加载、路由可达、PWA manifest）。
 *  - full：需要真实后端栈（API + SFU + Postgres + Redis），仅在 E2E_FULL=1 时执行，
 *    只跑 Chromium（Chromium 提供假摄像头/麦克风，可以验证真实媒体链路）。
 *
 * 本地：make e2e-up 启动依赖并按提示运行后端，然后
 *   cd frontend && E2E_FULL=1 npx playwright test --project=chromium
 * CI：.github/workflows/ci.yml 的 e2e-full job。
 *
 * 前端构建产物由本配置的 webServer（vite preview）提供；
 * 设置 E2E_BASE_URL 可改为测试一个已经在运行的前端（此时不再自动起 preview）。
 */
const fakeMediaArgs = [
  '--use-fake-device-for-media-stream',
  '--use-fake-ui-for-media-stream',
  '--autoplay-policy=no-user-gesture-required',
]

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
  webServer: process.env.E2E_BASE_URL
    ? undefined
    : {
        command: 'npm run preview -- --port 3000 --host 127.0.0.1',
        port: 3000,
        reuseExistingServer: !process.env.CI,
        timeout: 120_000,
      },
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        launchOptions: { args: fakeMediaArgs },
        permissions: ['camera', 'microphone'],
      },
    },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
    { name: 'webkit', use: { ...devices['Desktop Safari'] } },
  ],
})
