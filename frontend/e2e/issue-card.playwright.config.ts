import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from '@playwright/test'
import base from '../playwright.config'

const here = path.dirname(fileURLToPath(import.meta.url))
// 独立端口 + 独立 spec：不与共享 5173 端口、也不与 live 后端链路争用。
export default defineConfig({
  ...base,
  testDir: here,
  testMatch: 'issue-card-mock.spec.ts',
  use: { ...base.use, baseURL: 'http://127.0.0.1:5273' },
  webServer: {
    // Playwright 默认以配置文件所在目录为 cwd，这里显式回到 frontend 根。
    command: 'npx vite --config e2e/issue-card.vite.config.ts',
    cwd: path.resolve(here, '..'),
    url: 'http://127.0.0.1:5273/issue-card-preview.html',
    reuseExistingServer: false,
    timeout: 60_000,
  },
})
