import { expect, test } from 'vitest'
import { isArenaWorkspace, localFramework, sampleArguments } from './arena'

test('接受服务端已经完整提交的一轮，拒绝错误来源和超范围气势', () => {
  const round = {
    round_no: 1, pro_percent: 65, source: 'local',
    user_message_id: '94', assistant_message_id: '95', user_point: '先约定标准', assistant_point: '补充成本',
  }
  expect(isArenaWorkspace({ rounds: [round], hints: [{ round_no: 2, text: '准备反例', source: 'ai' }] })).toBe(true)
  expect(isArenaWorkspace({ rounds: [{ ...round, pro_percent: 150 }], hints: [] })).toBe(false)
  expect(isArenaWorkspace({ rounds: [{ ...round, source: 'unknown' }], hints: [] })).toBe(false)
  expect(isArenaWorkspace({ practice: true, rounds: [{ ...round, graph_source: 'ai' }], hints: [] })).toBe(true)
  expect(isArenaWorkspace({ rounds: [{ ...round, graph_source: 'unknown' }], hints: [] })).toBe(false)
})

test('两类本地辩词不同，框架保留可填空的三个部分', () => {
  expect(sampleArguments.strong).not.toBe(sampleArguments.flawed)
  expect(localFramework.match(/\[.+?\]/g)).toHaveLength(3)
})
