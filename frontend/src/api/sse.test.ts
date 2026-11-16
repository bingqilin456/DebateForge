import { describe, expect, it } from 'vitest'
import { readSSE } from './sse'

const encoder = new TextEncoder()
function bytes(text: string, size = 1) {
  const value = encoder.encode(text)
  return new ReadableStream<Uint8Array>({ start(controller) { for (let i = 0; i < value.length; i += size) controller.enqueue(value.slice(i, i + size)); controller.close() } })
}
describe('B2 SSE 增量解析', () => {
  it.each(['\n', '\r\n', '\r'])('逐字节跨中文/emoji/半行和半事件，兼容换行 %#', async newline => {
    const found: Array<[string, unknown]> = []
    const frame = ['\uFEFF: keepalive', '', 'event: delta', 'data: {"text":', 'data: "中文😀"}', '', 'event: done', 'data: {"ok":true}', '', ''].join(newline)
    await readSSE(bytes(frame), (event, data) => { found.push([event, data]) })
    expect(found).toEqual([['delta', { text: '中文😀' }], ['done', { ok: true }]])
  })
  it('不把注释、id/retry 或无 data 的帧作为事件/重连命令', async () => {
    const found: unknown[] = []
    await readSSE(bytes(': keepalive\n\nevent: start\n\nid: 12\nretry: 0\nevent: delta\ndata: {"text":" 一 "}\n\n'), (event, data) => { found.push([event, data]) })
    expect(found).toEqual([['delta', { text: ' 一 ' }]])
  })
  it('EOF 不分发没有末尾空行的半事件', async () => {
    const found: unknown[] = []
    await readSSE(bytes('event: done\ndata: {"ok":true}\n'), (_event, data) => { found.push(data) })
    expect(found).toEqual([])
  })
  it('严格拒绝损坏 UTF-8，不插入替代字符', async () => {
    const stream = new ReadableStream<Uint8Array>({ start(c) { c.enqueue(Uint8Array.from([0xe4, 0xb8])); c.close() } })
    await expect(readSSE(stream, () => {})).rejects.toMatchObject({ kind: 'format' })
  })
  it('损坏 JSON 受控失败，不暴露原文', async () => {
    await expect(readSSE(bytes('event: delta\ndata: private upstream payload\n\n'), () => {})).rejects.toMatchObject({ kind: 'format' })
  })
  it('限制未结束大帧，避免无限缓存', async () => {
    await expect(readSSE(bytes('data: '+ 'x'.repeat(70_000), 4096), () => {})).rejects.toMatchObject({ kind: 'format' })
  })
  it('终止事件只交付一次并关闭 reader，不处理迟到重复 done', async () => {
    const found: string[] = []
    await readSSE(bytes('event: done\ndata: {}\n\nevent: done\ndata: {}\n\n', 1000), (event) => { found.push(event); return true })
    expect(found).toEqual(['done'])
  })
  it('外部取消能结束悬挂读取', async () => {
    const controller = new AbortController()
    const read = readSSE(new ReadableStream<Uint8Array>({ start() {} }), () => {}, controller.signal)
    controller.abort()
    await expect(read).rejects.toMatchObject({ kind: 'network' })
  })
})
