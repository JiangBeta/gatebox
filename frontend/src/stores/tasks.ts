/**
 * 后台任务执行器（前端内存态）。
 *
 * 职责只有一件：把「耗时且不该阻塞界面」的写入包成后台执行，返回一个
 * 可 await 的句柄。**不再维护任务列表**——任务列表的唯一真相是后端 Run
 * （GET /api/v1/runs + SSE，顶栏角标与任务页都读它）。这里再存一份
 * 只会和真实 Run 对不上（刷新即失、跨浏览器看不到），所以删掉。
 *
 * 旧实现曾同时维护列表与角标，造成同一件事两套状态：写操作在内存里
 * 「运行中」，而真实 Run 可能早就失败。现已收敛到单一数据源。
 */

export interface TaskHandle {
  /** 任务名，仅用于日志与调试。 */
  name: string
  /** 完成时 resolve；失败时 reject（调用方需 catch）。 */
  done: Promise<void>
}

/**
 * runTask 以后台任务形式执行异步操作：立即返回句柄，不阻塞调用方。
 *
 * 与旧版的差异：不再往内存列表里插一条。失败照旧 reject，
 * 调用方负责提示（组件可能已随路由卸载，不能在组件里弹 message）。
 */
export function runTask(name: string, fn: () => Promise<void>): TaskHandle {
  const done = Promise.resolve().then(fn)
  if (import.meta.env.DEV) {
    done.catch(() => { /* DEV 下留下原始错误，便于定位；仍不吞掉 reject */ })
  }
  return { name, done }
}