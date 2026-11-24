import { defineConfig, devices } from '@playwright/test'

// V2-13 复盘的隔离浏览器 Mock 用例：独占 5174 端口，不占用共享的 5173，
// 也不启动后端、数据库或真实模型；所有响应由用例内 Mock 提供。
export default defineConfig({
  testDir: '.',
  testMatch: 'review-mock.spec.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 45_000,
  expect: { timeout: 10_000 },
  reporter: 'list',
  use: {
    baseURL: 'http://127.0.0.1:5174',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    timezoneId: 'Asia/Shanghai',
    // 本地可复用已安装的浏览器；不设置时使用 Playwright 自带 Chromium。
    launchOptions: process.env.SIYIGE_BROWSER_EXECUTABLE
      ? { executablePath: process.env.SIYIGE_BROWSER_EXECUTABLE } : undefined,
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'npm run dev -- --port 5174 --strictPort',
    url: 'http://127.0.0.1:5174',
    reuseExistingServer: false,
    timeout: 60_000,
  },
})
