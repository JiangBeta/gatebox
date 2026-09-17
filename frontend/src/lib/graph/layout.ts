/**
 * 手写分层布局（docs/v4/L1-05-topology.md §6）。
 *
 * BFS 深度分列 → 列内按父节点中心对齐 → 冲突下推。纯函数、确定性。
 */

export interface LayoutInput {
  nodes: { id: string }[]
  edges: { from: string; to: string }[]
}

export interface Position {
  x: number
  y: number
}

export interface LayoutOptions {
  xGap?: number
  yGap?: number
  nodeHeight?: number
}

export function layoutGraph(
  input: LayoutInput,
  opts: LayoutOptions = {},
): Record<string, Position> {
  const xGap = opts.xGap ?? 360
  const yGap = opts.yGap ?? 48
  const nodeHeight = opts.nodeHeight ?? 96

  const ids = input.nodes.map((n) => n.id).sort()
  const idSet = new Set(ids)
  const outgoing = new Map<string, string[]>()
  const incoming = new Map<string, string[]>()
  for (const id of ids) {
    outgoing.set(id, [])
    incoming.set(id, [])
  }
  for (const e of input.edges) {
    if (!idSet.has(e.from) || !idSet.has(e.to) || e.from === e.to) continue
    outgoing.get(e.from)!.push(e.to)
    incoming.get(e.to)!.push(e.from)
  }
  for (const list of outgoing.values()) list.sort()
  for (const list of incoming.values()) list.sort()

  // BFS 深度（无环路径的最长深度；回边不参与）
  const depth = new Map<string, number>()
  const queue: string[] = []
  for (const id of ids) {
    if (incoming.get(id)!.length === 0) {
      depth.set(id, 0)
      queue.push(id)
    }
  }
  if (queue.length === 0 && ids.length > 0) {
    depth.set(ids[0], 0)
    queue.push(ids[0])
  }
  // 标准 BFS：首次到达即定深度（visited 防环，避免环上无限增长）。
  while (queue.length > 0) {
    const cur = queue.shift()!
    const d = depth.get(cur)!
    for (const next of outgoing.get(cur)!) {
      if (depth.has(next)) continue
      depth.set(next, d + 1)
      queue.push(next)
    }
  }
  for (const id of ids) if (!depth.has(id)) depth.set(id, 0)

  // 分列
  const columns = new Map<number, string[]>()
  for (const id of ids) {
    const d = depth.get(id)!
    if (!columns.has(d)) columns.set(d, [])
    columns.get(d)!.push(id)
  }

  const pos: Record<string, Position> = {}
  const maxDepth = Math.max(0, ...columns.keys())
  for (let d = 0; d <= maxDepth; d++) {
    const col = (columns.get(d) ?? []).sort()
    if (col.length === 0) continue

    let placed: { id: string; y: number }[]
    if (d === 0) {
      placed = col.map((id, i) => ({ id, y: i * (nodeHeight + yGap) }))
    } else {
      const desired = col.map((id) => {
        const parents = incoming.get(id)!.map((p) => pos[p]).filter(Boolean)
        const base =
          parents.length > 0 ? parents.reduce((s, p) => s + p.y, 0) / parents.length : 0
        return { id, y: base }
      })
      desired.sort((a, b) => a.y - b.y || (a.id < b.id ? -1 : 1))
      placed = []
      let cursor = -Infinity
      for (const item of desired) {
        const y = Math.max(item.y, cursor)
        placed.push({ id: item.id, y })
        cursor = y + nodeHeight + yGap
      }
    }
    for (const item of placed) pos[item.id] = { x: d * xGap, y: item.y }
  }
  return pos
}
