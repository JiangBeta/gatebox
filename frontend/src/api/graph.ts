import http from './http'

/** 依赖图（v4，docs/v4/L1-02-graph.md）。 */

export type GraphView = 'component' | 'function'

export interface FactRef {
  kind: string
  id: string
}

export interface InfoCount {
  info: string
  count: number
  origins?: Record<string, number>
}

export interface GraphNode {
  id: string
  kind: string
  label: string
  tier?: string
  functions?: string[]
  implementors?: string[]
  consumes?: InfoCount[]
  produces?: InfoCount[]
}

export interface GraphEdge {
  from: string
  to: string
  info: string
  instances: FactRef[]
  cycle: boolean
}

export interface RefEdge {
  from: FactRef
  to: FactRef
  field: string
}

export interface GraphModel {
  view: GraphView
  nodes: GraphNode[]
  edges: GraphEdge[]
  factRefs: RefEdge[]
}

export async function getGraph(view: GraphView): Promise<GraphModel> {
  return (await http.get('/graph', { params: { view } })).data
}
