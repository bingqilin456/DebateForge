import type { AuthCredentials } from '../types/api'

// 密码绝不能 trim、截断或改变大小写；用户名按正式契约归一化。
export function normalizeCredentials(input: AuthCredentials): AuthCredentials {
  return { username: input.username.trim().toLowerCase(), password: input.password }
}

export function validateCredentials(input: AuthCredentials): string | null {
  const { username, password } = normalizeCredentials(input)
  if (!/^[a-z0-9_]{3,32}$/.test(username)) return '用户名须为 3–32 位 ASCII 字母、数字或下划线。'
  const length = [...password].length
  if (length < 12 || length > 128 || new TextEncoder().encode(password).length > 512) {
    return '密码须为 12–128 个 Unicode 字符且不超过 512 字节，首尾空白不会被移除。'
  }
  return null
}
