import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'

export default async function () {
  // 同一进程持有 Vite 实例并关闭，避免 Windows shell/npm 子进程树退出后遗留端口。
  // strictPort 拒绝接管其他服务；不启动/停止后端、代理或数据库。
  const server = await createServer({
    root: fileURLToPath(new URL('../', import.meta.url)),
    configFile: fileURLToPath(new URL('./b3.vite.config.ts', import.meta.url)),
    server: { host: '127.0.0.1', port: 5173, strictPort: true },
  })
  try { await server.listen() }
  catch (error) { await server.close(); throw error }
  return async () => { await server.close() }
}