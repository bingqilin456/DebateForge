// 按含义命名；相同数值不代表同一契约。字符数均按 Unicode 码点计算。
export const USERNAME_MIN_CHARS = 3
export const USERNAME_MAX_CHARS = 32
export const PASSWORD_MIN_CHARS = 12
export const PASSWORD_MAX_CHARS = 128
export const PASSWORD_MAX_BYTES = 512

export const USER_CONTENT_MAX_CHARS = 2000
export const AI_CONTENT_MAX_CHARS = 2000
export const TOPIC_TITLE_MAX_CHARS = 200
export const TOPIC_DESCRIPTION_MAX_CHARS = 2000
export const SUMMARY_TEXT_MAX_CHARS = 300
export const SUMMARY_LIST_MAX_ITEMS = 3
export const STRATEGY_TEXT_MAX_CHARS = 300
export const STRATEGY_LIST_MAX_ITEMS = 3
export const STRATEGY_CONTENT_MAX_BYTES = 8192
export const HISTORY_PAGE_SIZE = 20
export const HISTORY_MAX_PAGE_SIZE = 50

// 生成状态的只读轮询共用等待策略；与单次 HTTP 请求超时分别维护。
export const GENERATION_POLL_INTERVAL_MS = 2_000
export const GENERATION_POLL_BUDGET_MS = 120_000
