package caddy

import (
	"fmt"
	"strings"

	"github.com/JiangBeta/gatebox/internal/model"
)

// parseFragment 把片段体拆成 site 顶层行与 reverse_proxy 内部行(ADR-033 §1)。
//
// 判定(code 首个非空行的首个 token):
//   - `reverse_proxy { ... }`(不带参数):返回 rp 内部行(相对缩进),site 为 nil;
//   - 其它顶层指令:返回 site 顶层行,rp 为 nil;
//   - `reverse_proxy` 带参数 / 同片段混用顶层指令与 reverse_proxy 块:报错。
//
// 非反向代理服务上的 reverse_proxy 包裹片段:静默跳过
// (默认启用片段按类型无关 seed,生成器对不适用类型忽略,避免误报阻断生成)。
func parseFragment(code string, svc model.Service, appNames map[string]string, d model.ProxyDomain, varEnv map[string]string, staticRoot, dataDir string) (site, rp []string, err error) {
	lines, err := renderFragmentBody(code, svc, appNames, d, varEnv, staticRoot, dataDir)
	if err != nil {
		return nil, nil, err
	}
	if len(lines) == 0 {
		return nil, nil, nil
	}

	if fragmentFirstToken(lines[0]) != "reverse_proxy" {
		// 仅当 reverse_proxy 出现在顶层(depth 0)时才视为混用;
		// handle/route 块内部的 reverse_proxy 是合法的用户片段,不拦截。
		depth := 0
		for _, ln := range lines {
			t := strings.TrimSpace(ln)
			if strings.HasPrefix(t, "}") && depth > 0 {
				depth--
			}
			if depth == 0 && fragmentFirstToken(t) == "reverse_proxy" {
				return nil, nil, fmt.Errorf("片段不能同时包含顶层指令与 reverse_proxy 块")
			}
			if strings.HasSuffix(t, "{") {
				depth++
			}
		}
		return lines, nil, nil
	}

	// 必须是裸 `reverse_proxy {`(不带上游/matcher 等参数)。
	if strings.NewReplacer(" ", "", "\t", "").Replace(strings.TrimSpace(lines[0])) != "reverse_proxy{" {
		return nil, nil, fmt.Errorf("reverse_proxy 片段不能带参数(上游由服务决定)")
	}
	if strings.TrimSpace(lines[len(lines)-1]) != "}" {
		return nil, nil, fmt.Errorf("reverse_proxy 片段缺少结尾 }")
	}
	if svc.Type != model.RouteTypeReverseProxy {
		return nil, nil, nil // 不适用类型:静默跳过
	}
	return nil, normalizeIndent(lines[1 : len(lines)-1]), nil
}

// fragmentFirstToken 取行(去首尾空白后)的首个 token;遇空白或 '{' 截止。
func fragmentFirstToken(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	for i, r := range line {
		if r == ' ' || r == '\t' || r == '{' {
			return line[:i]
		}
	}
	return line
}

// normalizeIndent 按花括号深度重排内部行的制表符缩进(顶层 0 个 tab)。
func normalizeIndent(lines []string) []string {
	out := make([]string, 0, len(lines))
	depth := 0
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "}") && depth > 0 {
			depth--
		}
		out = append(out, strings.Repeat("\t", depth)+t)
		if strings.HasSuffix(t, "{") {
			depth++
		}
	}
	return out
}

// hasReverseProxyDirective 判断 rp 内部行(相对缩进)顶层是否含指定子指令。
func hasReverseProxyDirective(lines []string, name string) bool {
	for _, ln := range lines {
		if strings.HasPrefix(ln, "\t") {
			continue // 仅看顶层(0 缩进)
		}
		t := strings.TrimSpace(ln)
		if t == name || strings.HasPrefix(t, name+" ") || strings.HasPrefix(t, name+"{") {
			return true
		}
	}
	return false
}
