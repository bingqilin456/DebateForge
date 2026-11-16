import { USERNAME_MIN_CHARS, USERNAME_MAX_CHARS, PASSWORD_MIN_CHARS, PASSWORD_MAX_CHARS, PASSWORD_MAX_BYTES } from '../shared/limits'
import type { AuthCredentials } from '../types/api'

export const USERNAME_PATTERN = new RegExp(`^[a-z0-9_]{${USERNAME_MIN_CHARS},${USERNAME_MAX_CHARS}}$`)

// 密码绝不能 trim、截断或改变大小写；用户名按正式契约归一化。
export function normalizeCredentials(input: AuthCredentials): AuthCredentials {
  return { username: input.username.trim().toLowerCase(), password: input.password }
}

export function validateCredentials(input: AuthCredentials): string | null {
  const { username, password } = normalizeCredentials(input)
  if (!USERNAME_PATTERN.test(username)) return `用户名须为 ${USERNAME_MIN_CHARS}–${USERNAME_MAX_CHARS} 位 ASCII 字母、数字或下划线。`
  const length = [...password].length
  if (length < PASSWORD_MIN_CHARS || length > PASSWORD_MAX_CHARS || new TextEncoder().encode(password).length > PASSWORD_MAX_BYTES) {
    return `密码须为 ${PASSWORD_MIN_CHARS}–${PASSWORD_MAX_CHARS} 个 Unicode 字符且不超过 ${PASSWORD_MAX_BYTES} 字节，首尾空白不会被移除。`
  }
  return null
}
