// 存活检查不等于数据库、模型或完整应用就绪。
export interface LiveHealth {
  status: 'alive'
}

export interface AuthCredentials {
  username: string
  password: string
}

export interface User {
  readonly id: string
  readonly username: string
}

// 只包含 CSRF token；Session 原值只由浏览器处理 HttpOnly Cookie。
export interface AuthSession {
  readonly user: User
  readonly csrf_token: string
}
