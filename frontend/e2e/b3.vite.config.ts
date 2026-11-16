import { mergeConfig } from 'vite'
import base from '../vite.config'

// 仅 B3 本地验收：保留页面及同源 Origin，只把原 /api/ 代理经过实际 Nginx。
// 不改变正常 npm run dev 使用的配置，也不拦截/拼装浏览器收到的 SSE body。
export default mergeConfig(base, {
  server: { proxy: { '/api/': { target: 'http://127.0.0.1:18080' } } },
})