import { createServer, type ServerResponse } from 'node:http'
import { once } from 'node:events'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { getLiveHealth } from './health'
import { requestJson } from './client'

const server = createServer((req, res: ServerResponse) => {
  if (req.url === '/unavailable') {
    res.writeHead(503, { 'Content-Type': 'application/json' })
    res.end('{"error":"private-provider-detail"}')
  } else if (req.url === '/html') {
    res.writeHead(200, { 'Content-Type': 'text/html' })
    res.end('<html>SPA fallback</html>')
  } else if (req.url === '/invalid-json') {
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end('{broken')
  } else if (req.url === '/wrong-status') {
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end('{"status":"ready"}')
  } else if (req.url === '/slow-headers') {
    const timer = setTimeout(() => res.end('{"status":"alive"}'), 200)
    res.on('close', () => clearTimeout(timer))
  } else if (req.url === '/slow-body') {
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.write('{"status":')
    const timer = setTimeout(() => res.end('"alive"}'), 200)
    res.on('close', () => clearTimeout(timer))
  } else if (req.url === '/api/health/live') {
    res.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8' })
    res.end('{"status":"alive"}')
  } else {
    res.writeHead(404, { 'Content-Type': 'application/json' })
    res.end('{"error":{"code":"not_found","message":"接口不存在"},"request_id":"test-request"}')
  }
})
let baseUrl: string

beforeAll(async () => {
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  const address = server.address()
  if (address === null || typeof address === 'string') throw new Error('测试监听失败')
  baseUrl = `http://127.0.0.1:${address.port}`
})

afterAll(async () => {
  server.closeAllConnections()
  await new Promise<void>((resolve, reject) => server.close((err) => err ? reject(err) : resolve()))
})

describe('真实 HTTP 健康客户端', () => {
  it('只把有效 alive JSON 判为存活', async () => {
    await expect(getLiveHealth(`${baseUrl}/api/health/live`)).resolves.toEqual({ status: 'alive' })
  })

  it('默认请求能访问当前注册的健康路径', async () => {
    const realFetch = globalThis.fetch
    // Node 没有页面 Origin；只补浏览器的相对 URL 解析，仍走真实 HTTP 和客户端。
    vi.stubGlobal('fetch', (input: Parameters<typeof fetch>[0], init?: RequestInit) =>
      realFetch(typeof input === 'string' ? new URL(input, baseUrl) : input, init))
    try {
      await expect(getLiveHealth()).resolves.toEqual({ status: 'alive' })
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('HTTP 失败只暴露受控提示，不泄露服务端正文', async () => {
    await expect(requestJson(`${baseUrl}/unavailable`)).rejects.toMatchObject({ kind: 'http', message: '服务返回 HTTP 503，请确认后端已启动后重试。' })
  })

  it.each(['/html', '/invalid-json', '/wrong-status'])('拒绝伪成功响应 %s', async (path) => {
    await expect(getLiveHealth(`${baseUrl}${path}`)).rejects.toMatchObject({ kind: 'format' })
  })

  it.each(['/slow-headers', '/slow-body'])('总超时涵盖请求及响应体 %s', async (path) => {
    await expect(getLiveHealth(`${baseUrl}${path}`, 30)).rejects.toMatchObject({ kind: 'timeout' })
  })

  it('网络不可达时返回受控失败', async () => {
    await expect(requestJson('http://127.0.0.1:0/api/health/live')).rejects.toMatchObject({ kind: 'network' })
  })

  it('失败后允许显式发起新的成功请求', async () => {
    await expect(getLiveHealth(`${baseUrl}/unavailable`)).rejects.toMatchObject({ kind: 'http' })
    await expect(getLiveHealth(`${baseUrl}/api/health/live`)).resolves.toEqual({ status: 'alive' })
  })
})
