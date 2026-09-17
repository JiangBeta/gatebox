import { describe, expect, it } from 'vitest'
import { layoutGraph } from './layout'

describe('layoutGraph', () => {
  it('线性链按深度递增 x，且同列不重叠', () => {
    const pos = layoutGraph({
      nodes: [{ id: 'a' }, { id: 'b' }, { id: 'c' }],
      edges: [
        { from: 'a', to: 'b' },
        { from: 'b', to: 'c' },
      ],
    })
    expect(pos.a.x).toBeLessThan(pos.b.x)
    expect(pos.b.x).toBeLessThan(pos.c.x)
  })

  it('分叉后的汇聚列不重叠', () => {
    const pos = layoutGraph({
      nodes: [{ id: 'root' }, { id: 'a' }, { id: 'b' }, { id: 'sink' }],
      edges: [
        { from: 'root', to: 'a' },
        { from: 'root', to: 'b' },
        { from: 'a', to: 'sink' },
        { from: 'b', to: 'sink' },
      ],
    })
    expect(pos.a.y).not.toBe(pos.b.y)
    expect(Math.abs(pos.a.y - pos.b.y)).toBeGreaterThanOrEqual(96)
  })

  it('自环/回边不参与分层，且结果确定', () => {
    const input = {
      nodes: [{ id: 'a' }, { id: 'b' }],
      edges: [
        { from: 'a', to: 'b' },
        { from: 'b', to: 'a' },
        { from: 'a', to: 'a' },
      ],
    }
    const p1 = layoutGraph(input)
    const p2 = layoutGraph(input)
    expect(p1).toEqual(p2)
    expect(p1.a.x).toBeLessThan(p1.b.x)
  })

  it('空图返回空坐标', () => {
    expect(layoutGraph({ nodes: [], edges: [] })).toEqual({})
  })
})
