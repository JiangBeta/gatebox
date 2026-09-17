package reconcile

import (
	"sort"

	"github.com/JiangBeta/gatebox/internal/graph"
)

// Order 对给定组件按信息流边做拓扑排序（生产者先于消费者）。
//
// 仅考虑 ids 内的节点；环（回边）不阻塞：Kahn 处理后剩余节点按 id 稳定追加。
func Order(ids []string, edges []graph.Edge) []string {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	indeg := make(map[string]int, len(ids))
	adj := make(map[string][]string, len(ids))
	for _, id := range ids {
		indeg[id] = 0
	}
	seen := map[[2]string]bool{}
	for _, e := range edges {
		if !set[e.From] || !set[e.To] || e.From == e.To {
			continue
		}
		key := [2]string{e.From, e.To}
		if seen[key] {
			continue
		}
		seen[key] = true
		adj[e.From] = append(adj[e.From], e.To)
		indeg[e.To]++
	}

	queue := make([]string, 0)
	for _, id := range ids {
		if indeg[id] == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)

	out := make([]string, 0, len(ids))
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		out = append(out, cur)
		next := append([]string(nil), adj[cur]...)
		sort.Strings(next)
		for _, n := range next {
			indeg[n]--
			if indeg[n] == 0 {
				queue = append(queue, n)
				sort.Strings(queue)
			}
		}
	}
	if len(out) < len(ids) {
		// 存在环：剩余节点稳定追加。
		done := map[string]bool{}
		for _, id := range out {
			done[id] = true
		}
		rest := make([]string, 0)
		for _, id := range ids {
			if !done[id] {
				rest = append(rest, id)
			}
		}
		sort.Strings(rest)
		out = append(out, rest...)
	}
	return out
}
