import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from '@playwright/test'
import base from '../playwright.config'

const here = path.dirname(fileURLToPath(import.meta.url))
// 配置会在主进程/worker 再次加载；共享本次 ID，保证报告与截图落在同一目录。
const runId = process.env.SIYIGE_B3_RUN_ID ?? new Date().toISOString().replace(/[:.]/g, '-')
if (!/^\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}-\d{3}Z$/.test(runId)) throw new Error('B3 运行 ID 格式不合法')
process.env.SIYIGE_B3_RUN_ID = runId
const output = path.resolve(here, '../../.cache/b3-browser', runId)
// defineConfig 多参数会拼接 webServer，故用单个对象替换，而不是启动两台服务器。
export default defineConfig({
  ...base,
  metadata: { ...base.metadata, b3_run_id: runId },
  testDir: here,
  testMatch: 'b3-live.spec.ts',
  outputDir: output,
  retries: 0,
  workers: 1,
  reporter: [['list'], ['json', { outputFile: path.join(output, 'results.json') }]],
  use: { ...base.use, trace: 'off', screenshot: 'off', timezoneId: 'Asia/Shanghai' },
  // 明确移除普通入口的 webServer，专项 setup 只持有本次自己的 Vite。
  webServer: undefined,
  globalSetup: path.join(here, 'b3.setup.ts'),
})
