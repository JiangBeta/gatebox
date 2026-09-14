package caddy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/JiangBeta/gatebox/internal/models"
)

// gateboxVar 匹配 <%SOMETHING%> 形式的 GateBox 变量(与 caddy env ${...} 区分)。
var gateboxVar = regexp.MustCompile(`<%([^%>]+)%>`)

// 内建只读变量的 key(ADR-018 修订 §5.1,定界符 <% %>)。
const (
	VarAppName     = "<%GB_APP%>"
	VarServiceName = "<%GB_SERVICE%>"
	VarStaticRoot  = "<%GB_STATIC_ROOT%>"
	VarHostPort    = "<%GB_HOST_PORT%>"
)

// interpolateVars 把 s 中的 GateBox 变量替换为实际值:
//   - 内建:GB_APP(应用名)/GB_SERVICE(服务名)/GB_STATIC_ROOT(全局静态根)/GB_HOST_PORT(反代目标端口)
//   - 用户变量:varEnv(API 层并入)
//   - 未命中的 <%...%> 视为配置错误(防拼错静默生成非法路径)。
//   - ${...}(无 <%>)原样保留,交给 caddy 运行时读环境变量(透传)。
func interpolateVars(s string, svc models.Service, appNames map[string]string, d models.ProxyDomain, varEnv map[string]string) (string, error) {
	if !strings.Contains(s, "<%") {
		return s, nil
	}
	hostPort := ""
	if len(svc.Upstream) > 0 {
		hostPort = upstreamPort(svc.Upstream[0])
	}
	appName := appNames[svc.AppID]

	var buildErr error
	out := gateboxVar.ReplaceAllStringFunc(s, func(m string) string {
		var key string
		switch m {
		case VarAppName:
			key = appName
		case VarServiceName:
			key = svc.Name
		case VarHostPort:
			key = hostPort
		default:
			if v, ok := varEnv[m]; ok {
				key = v
			} else if strings.HasPrefix(m, "<%GB_") {
				// 未定义的保留变量(仅静态根是全局变量,由调用方在 root 替换前注入)
				key = m
			} else {
				buildErr = fmt.Errorf("未定义的变量 %s", m)
				return m
			}
		}
		return key
	})
	if buildErr != nil {
		return "", buildErr
	}
	return out, nil
}

// upstreamPort 从 "地址:端口" 提取端口。
func upstreamPort(up string) string {
	idx := strings.LastIndex(up, ":")
	if idx < 0 {
		return ""
	}
	return up[idx+1:]
}

// renderFragmentBody 把片段体渲染成 Caddyfile 行。
// 先后:变量插值 → 按行拆分为非空行(供调用方缩进)。
func renderFragmentBody(code string, svc models.Service, appNames map[string]string, d models.ProxyDomain, varEnv map[string]string) ([]string, error) {
	interp, err := interpolateVars(code, svc, appNames, d, varEnv)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, ln := range strings.Split(interp, "\n") {
		t := strings.TrimRight(ln, " \t")
		if strings.TrimSpace(t) == "" {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// BuiltinFragmentCatalog 只读内置 Caddy 片段清单(ADR-018 修订 Q7,8 个,不可改)。
// code 为示例片段体,遵循 caddy Caddyfile 语法,可含 <%VAR%>。
func BuiltinFragmentCatalog() []models.FragmentTemplate {
	return []models.FragmentTemplate{
		{
			ID:          models.FragmentGzip,
			Name:        "Gzip/Br 压缩",
			Description: "对响应启用 gzip/zstd/brotli 压缩",
			Code:        "encode gzip zstd",
		},
		{
			ID:             models.FragmentBasicAuth,
			Name:           "Basic Auth 认证",
			Description:    "HTTP Basic 认证保护(参数改后请更新)",
			DefaultEnabled: false,
			Code:           "basic_auth {\n\tadmin $2a$10$REPLACE_WITH_BCRYPT\n}",
		},
		{
			ID:          models.FragmentSkipVerify,
			Name:        "忽略自带证书校验",
			Description: "反向代理到自签/内网 https 接口时跳过证书校验",
			Code:        "tls_connection_policies {\n\tmatch_sni *\n}" + "\n" + "reverse_proxy { ... }",
		},
		{
			ID:          models.FragmentHealthcheck,
			Name:        "健康检查",
			Description: "启用 active health check(探测 /)",
			Code:        "",
		},
		{
			ID:          models.FragmentLogConfig,
			Name:        "日志配置",
			Description: "为请求记录访问日志(DEBUG 级)",
			Code:        "log {\n\toutput stdout\n\tlevel debug\n}",
		},
		{
			ID:             models.FragmentBlockCommon,
			Name:           "阻止常见漏洞",
			Description:    "注入常见安全响应头并限制危险方法",
			DefaultEnabled: true,
			Code:           "header {\n\t-X-Powered-By\n\tX-Content-Type-Options nosniff\n\tX-Frame-Options SAMEORIGIN\n}",
		},
		{
			ID:             models.FragmentStaticCache,
			Name:           "静态资源缓存",
			Description:    "对静态资源设置长缓存",
			DefaultEnabled: false,
			Code:           "header {\n\tCache-Control no-cache\n}",
		},
		{
			ID:          models.FragmentWebsocket,
			Name:        "支持 websocket",
			Description: "支持 WebSocket 升级(caddy reverse_proxy 默认支持,此片段标记用途)",
			Code:        "",
		},
	}
}

// DefaultEnabledFragmentIDs 返回应默认启用(默认勾选且非隐藏)的内置/片段 ID。
// 由 API 层在创建 Service 时 seed 进 FragmentIDs;含 hidden 的强加片段也在此。
func DefaultEnabledFragmentIDs(fragments []models.Fragment, builtins []models.FragmentTemplate) []string {
	var ids []string
	for _, f := range fragments {
		if f.DefaultEnabled {
			ids = append(ids, f.ID)
		}
	}
	for _, b := range builtins {
		if b.DefaultEnabled {
			ids = append(ids, b.ID)
		}
	}
	sortAndDedup(ids)
	return ids
}

func sortAndDedup(ids []string) []string {
	if len(ids) == 0 {
		return ids
	}
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
