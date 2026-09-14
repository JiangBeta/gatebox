/**
 * 跨模块共享的类型与常量（只被依赖，不依赖任何层）。
 */

/** 统一错误响应体（docs/architecture.md §10）。 */
export interface ApiError {
  error: {
    code: string
    message: string
  }
}

/** 分页响应体。 */
export interface Page<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}
