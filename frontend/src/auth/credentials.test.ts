import { describe, expect, it } from 'vitest'
import { normalizeCredentials, validateCredentials } from './credentials'

const valid = { username: ' Alice_1 ', password: 'example-password' }
describe('认证输入约束', () => {
  it('仅归一化用户名；密码首尾空白原样保留', () => {
    expect(normalizeCredentials({ ...valid, password: '  example-password  ' }))
      .toEqual({ username: 'alice_1', password: '  example-password  ' })
  })
  it.each(['ab', 'a'.repeat(33), '用户abc', 'a-b', 'a b', ''])('拒绝非法用户名 %s', (username) => {
    expect(validateCredentials({ ...valid, username })).toBeTypeOf('string')
  })
  it.each(['a'.repeat(11), 'a'.repeat(129), '😀'.repeat(129)])('拒绝越界密码 %#', (password) => {
    expect(validateCredentials({ ...valid, password })).toBeTypeOf('string')
  })
  it.each(['a'.repeat(12), 'a'.repeat(128), '😀'.repeat(12), '😀'.repeat(128), ' '.repeat(12)])
  ('按 Unicode 码点而非 UTF-16 长度校验，不截断密码 %#', (password) => {
    expect(validateCredentials({ ...valid, password })).toBeNull()
  })
})
