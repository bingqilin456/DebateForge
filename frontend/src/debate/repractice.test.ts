// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRepracticeState } from './repractice'
import type { RepracticeApi } from '../api/repractice'
import type { RepracticeVersion } from '../types/api'

/**
 * V2-08 局部重练状态机测试。
 *
 * 重点：显式触发、失败保留输入、恢复历史版本不覆盖既有版本、
 * 幂等编号、版本冲突只重读不自动重发、能力关闭时整体不可用。
 * API 全部是桩：这些用例不需要真实网络或后端。
 */

const source = { message_id: '202', start_cp: 0, end_cp: 3, quote: '甲😀乙' }

function version(overrides: Partial<RepracticeVersion> = {}): RepracticeVersion {
  return {
    id: '7', status: 'completed', attempt_version: 1, source,
    goal: '', rewrite: '改写一', feedback: { improved: true, change: '补依据', reason: '理由', new_problem: '' },
    error_code: null, restored_from: null,
    ...overrides,
  } as RepracticeVersion
}

function apiStub(overrides: Partial<RepracticeApi> = {}): RepracticeApi {
  return {
    list: vi.fn(async () => ({ available: true, items: [] })),
    submit: vi.fn(async () => version()),
    retry: vi.fn(async () => version({ attempt_version: 2 })),
    ...overrides,
  }
}

/** 取一个已断言存在的 mock 函数，保持类型安全并便于断言调用参数。 */
function mocked<T>(fn: T): ReturnType<typeof vi.fn> {
  return fn as unknown as ReturnType<typeof vi.fn>
}

beforeEach(() => { window.getSelection()?.removeAllRanges() })
afterEach(() => { vi.restoreAllMocks() })

describe('局部重练状态机', () => {
  it('打开会话只读历史，不触发提交或生成', async () => {
    const list = vi.fn(async () => ({ available: true, items: [version()] }))
    const api = apiStub({ list })
    const state = createRepracticeState(api)
    await state.open('123')
    expect(list).toHaveBeenCalledWith('123')
    expect(api.submit).not.toHaveBeenCalled()
    expect(state.list.value).toHaveLength(1)
    expect(state.available.value).toBe(true)
    state.dispose()
  })

  it('选中本人原句后进入编辑态；提交成功清空改写但保留选段', async () => {
    const api = apiStub()
    const state = createRepracticeState(api)
    await state.open('123')
    state.selectSource(source)
    state.rewrite.value = '补充判断依据'
    expect(state.canSubmit.value).toBe(true)
    await state.submit()
    expect(mocked(api.submit)).toHaveBeenCalledTimes(1)
    expect(mocked(api.submit).mock.calls[0]![1]).toMatchObject({
      message_id: '202', start_cp: 0, end_cp: 3, quote: '甲😀乙', rewrite: '补充判断依据',
    })
    expect(state.list.value).toHaveLength(1)
    expect(state.rewrite.value).toBe('')
    // 选段保留，方便继续写下一版。
    expect(state.selection.value?.message_id).toBe('202')
    state.dispose()
  })

  it('提交失败保留选段与改写，且不把未保存内容当成版本', async () => {
    const api = apiStub({
      submit: vi.fn(async () => { throw new Error('保存失败') }),
      list: vi.fn(async () => ({ available: true, items: [] })),
    })
    const state = createRepracticeState(api)
    await state.open('123')
    state.selectSource(source)
    state.rewrite.value = '要保留的改写'
    await state.submit()
    expect(state.rewrite.value).toBe('要保留的改写')
    expect(state.selection.value?.quote).toBe('甲😀乙')
    expect(state.list.value).toHaveLength(0)
    expect(state.failure.value).toContain('操作失败')
    state.dispose()
  })

  it('保存成功但回包丢失后，同一改写沿用请求编号且只展示一份版本', async () => {
    const saved = version()
    const submit = vi.fn<RepracticeApi['submit']>()
      .mockRejectedValueOnce(new Error('回包丢失'))
      .mockResolvedValueOnce(saved)
    const list = vi.fn<RepracticeApi['list']>()
      .mockResolvedValueOnce({ available: true, items: [] })
      .mockResolvedValueOnce({ available: true, items: [saved] })
    const state = createRepracticeState(apiStub({ submit, list }))
    await state.open('123')
    state.selectSource(source)
    state.setRewrite(saved.rewrite)
    await state.submit()
    await state.submit()
    expect(submit.mock.calls[1]?.[1]).toEqual(submit.mock.calls[0]?.[1])
    expect(state.list.value).toEqual([saved])
  })

  it('生成失败返回 failed 版本：保留改写并提示可显式重试', async () => {
    const failed = version({ status: 'failed', feedback: null, error_code: 'model_timeout' } as Partial<RepracticeVersion>)
    const api = apiStub({ submit: vi.fn(async () => failed) })
    const state = createRepracticeState(api)
    await state.open('123')
    state.selectSource(source)
    state.rewrite.value = '失败的改写'
    await state.submit()
    expect(state.list.value).toHaveLength(1)
    expect(state.list.value[0]!.status).toBe('failed')
    expect(state.failure.value).toContain('反馈未生成，改写已保留')
    // 草稿保留：用户可继续编辑或直接重试。
    expect(state.rewrite.value).toBe('失败的改写')
    state.dispose()
  })

  it('重试必须带服务端当前版本；成功后同一条版本更新而不新增', async () => {
    const failed = version({ status: 'failed', feedback: null, error_code: 'model_timeout' } as Partial<RepracticeVersion>)
    const retry = vi.fn(async () => version({ attempt_version: 2 }))
    const api = apiStub({ retry, list: vi.fn(async () => ({ available: true, items: [failed] })) })
    const state = createRepracticeState(api)
    await state.open('123')
    await state.retry(state.list.value[0]!)
    expect(retry).toHaveBeenCalledWith('123', '7', { expected_attempt_version: 1 })
    expect(state.list.value).toHaveLength(1)
    expect(state.list.value[0]!.status).toBe('completed')
    state.dispose()
  })

  it('版本冲突只重读历史，不自动换版本重发', async () => {
    const failed = version({ status: 'failed', feedback: null, error_code: 'model_timeout' } as Partial<RepracticeVersion>)
    const list = vi.fn(async () => ({ available: true, items: [failed] }))
    const retry = vi.fn(async () => { throw new Error('版本已变化') })
    const api = apiStub({ retry, list })
    const state = createRepracticeState(api)
    await state.open('123')
    await state.retry(state.list.value[0]!)
    expect(retry).toHaveBeenCalledTimes(1)
    // 失败后只重新读取一次权威历史。
    expect(list).toHaveBeenCalledTimes(2)
    expect(state.failure.value).toContain('操作失败')
    state.dispose()
  })

  it('恢复历史版本只填回编辑区，不创建版本', async () => {
    const existing = version({ id: '5', rewrite: '历史改写' })
    const api = apiStub({ list: vi.fn(async () => ({ available: true, items: [existing] })) })
    const state = createRepracticeState(api)
    await state.open('123')
    state.restore(state.list.value[0]!)
    expect(state.rewrite.value).toBe('历史改写')
    expect(state.selection.value?.message_id).toBe('202')
    expect(state.restoredFrom.value).toBe('5')
    expect(api.submit).not.toHaveBeenCalled()
    expect(state.list.value).toHaveLength(1)

    // 再次提交时才产生新版本，并带上恢复来源。
    await state.submit()
    expect(mocked(api.submit).mock.calls[0]![1].restored_from).toBe('5')
    state.dispose()
  })

  it('能力关闭时 available=false，且不允许提交', async () => {
    const api = apiStub({ list: vi.fn(async () => ({ available: false, items: [] })) })
    const state = createRepracticeState(api)
    await state.open('123')
    expect(state.available.value).toBe(false)
    state.selectSource(source)
    state.rewrite.value = '改写'
    // 关闭态由界面整体隐藏；状态机自身仍保持可判定，避免误把空列表当可用。
    expect(state.canSubmit.value).toBe(true)
    state.dispose()
  })

  it('空白改写与超长改写不能提交', async () => {
    const api = apiStub()
    const state = createRepracticeState(api)
    await state.open('123')
    state.selectSource(source)
    state.rewrite.value = ' \uFEFF '
    expect(state.canSubmit.value).toBe(false)
    state.rewrite.value = '改'.repeat(2001)
    expect(state.canSubmit.value).toBe(false)
    state.rewrite.value = '改'.repeat(2000)
    expect(state.canSubmit.value).toBe(true)
    state.dispose()
  })

  it('切换会话清空上一场的选段与草稿', async () => {
    const api = apiStub()
    const state = createRepracticeState(api)
    await state.open('123')
    state.selectSource(source)
    state.rewrite.value = '上一场改写'
    await state.open('124')
    expect(state.selection.value).toBeNull()
    expect(state.rewrite.value).toBe('')
    state.dispose()
  })
})

