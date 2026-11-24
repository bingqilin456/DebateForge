import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 本机演示只使用样例，切勿添加正式 API 代理。
export default defineConfig({
  plugins: [vue()],
  server: { host: '127.0.0.1', port: 5273, strictPort: true },
})
