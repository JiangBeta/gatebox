package extension

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// VariantKey 计算核心组件配方变体键。
//
// **算法必须与 GateBoxStore 的 `gbx-store variant-key` 完全一致**（ADR-038 §2、
// docs/catalog.md §3）：特征去重后按字节序升序，逐行拼接后取 sha256 前 16 位十六进制。
func VariantKey(component, version, osName, arch string, features []string) string {
	seen := map[string]bool{}
	fs := make([]string, 0, len(features))
	for _, f := range features {
		f = strings.TrimSpace(f)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		fs = append(fs, f)
	}
	sort.Strings(fs)

	var b strings.Builder
	b.WriteString("component=" + strings.TrimSpace(component) + "\n")
	b.WriteString("version=" + strings.TrimSpace(version) + "\n")
	b.WriteString("os=" + strings.TrimSpace(osName) + "\n")
	b.WriteString("arch=" + strings.TrimSpace(arch) + "\n")
	for _, f := range fs {
		b.WriteString("feature=" + f + "\n")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])[:16]
}

// ComponentFeatures 返回某核心组件当前启用的特征并集（去重、稳定排序）。
//
// 特征来自已注册提供者的 `component-variant` 能力；核心只认识特征集合，
// 不认识具体插件身份（ADR-036 I1）。
func (r *Registry) ComponentFeatures(component string) []string {
	seen := map[string]bool{}
	for _, c := range r.CapabilitiesByPoint(PointComponentVar) {
		if stringField(c.Meta, "component") != component {
			continue
		}
		if f := stringField(c.Meta, "feature"); f != "" {
			seen[f] = true
		}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}
