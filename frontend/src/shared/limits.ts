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
/** PRD §12：自定义辩题 1–200 字。 */
export const CUSTOM_TOPIC_TITLE_MAX_CHARS = 200
/** PRD §12：自定义辩题背景选填，最多 1000 字。 */
export const CUSTOM_TOPIC_BACKGROUND_MAX_CHARS = 1000
export const SUMMARY_TEXT_MAX_CHARS = 300
export const SUMMARY_LIST_MAX_ITEMS = 3
export const STRATEGY_TEXT_MAX_CHARS = 300
export const STRATEGY_LIST_MAX_ITEMS = 3
export const STRATEGY_CONTENT_MAX_BYTES = 8192
// V2-13 复盘：与总结同名数值不代表同一契约，独立常量避免误改。
export const REVIEW_TEXT_MAX_CHARS = 300
export const REVIEW_KEY_ISSUES_MAX_ITEMS = 5
export const REVIEW_LIST_MAX_ITEMS = 3
export const REVIEW_CONTENT_MAX_BYTES = 24 * 1024
export const HISTORY_PAGE_SIZE = 20
export const HISTORY_MAX_PAGE_SIZE = 50

// V2-08 局部重练：改写与改进目标各自按 Unicode 码点计，与个人发言同规则。
export const REPRACTICE_REWRITE_MAX_CHARS = 2000
export const REPRACTICE_GOAL_MAX_CHARS = 2000
/** 请求编号上限，与服务端契约一致。 */
export const REPRACTICE_REQUEST_ID_MAX_CHARS = 128

// 生成状态的只读轮询共用等待策略；与单次 HTTP 请求超时分别维护。
export const GENERATION_POLL_INTERVAL_MS = 2_000
export const GENERATION_POLL_BUDGET_MS = 120_000
