package caddy

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/JiangBeta/gatebox/internal/model"
)

// gateboxVar 匹配 <%SOMETHING%> 形式的 GateBox 变量(与 caddy env {$...} 区分)。
var gateboxVar = regexp.MustCompile(`<%([^%>]+)%>`)

// 内建只读变量的 key(ADR-018 修订 §5.1 / ADR-033,定界符 <% %>)。
// 最小变量集:GB_DATA_DIR(路径原语) + 服务上下文(APP/SERVICE/HOST_PORT);
// GB_STATIC_ROOT / GB_LOG_FILE 由其组合定义(见 SystemVariables)。
const (
	VarAppName     = "<%GB_APP%>"
	VarServiceName = "<%GB_SERVICE%>"
	VarHostPort    = "<%GB_HOST_PORT%>"
	VarDataDir     = "<%GB_DATA_DIR%>"
	VarStaticRoot  = "<%GB_STATIC_ROOT%>"
	VarLogFile     = "<%GB_LOG_FILE%>"
	// VarSubDomain 当前域名行的完整访问域名(<subdomain>.<rootDomain>)。
	VarSubDomain = "<%GB_SUB_DOMAIN%>"
	// VarACMEFile acme.sh 工作目录(= <GB_DATA_DIR>/tools/acme,证书根)。
	VarACMEFile = "<%GB_ACME_FILE%>"
	// VarSSLFile 当前域名行的 fullchain 证书路径。
	VarSSLFile = "<%GB_SSL_FILE%>"
)

// acmeHome 返回 acme.sh 工作目录(证书根 <home>/certs 的上一级)。
func acmeHome(dataDir string) string {
	return filepath.Join(dataDir, "tools", "acme")
}

// SystemVariables 返回内置系统变量视图(供「变量」页只读展示,ADR-033 §3)。
// 仅 GB_DATA_DIR 解析为实际绝对路径;组合路径用 <%...%> 表达;
// 服务/域名上下文变量保留 <%…%> 占位。顺序即展示顺序(从最小变量到组合变量)。
func SystemVariables(dataDir, staticRoot string) []model.Variable {
	// static_root 默认 = <dataDir>/www 时用 <%GB_DATA_DIR%> 表达;自定义路径则显示实际值。
	staticRootDisplay := staticRoot
	if staticRoot == filepath.Join(dataDir, "www") {
		staticRootDisplay = filepath.Join(VarDataDir, "www")
	}
	return []model.Variable{
		{Key: "GB_APP", Value: VarAppName, Description: "当前应用名称(按服务上下文解析)"},
		{Key: "GB_SERVICE", Value: VarServiceName, Description: "当前服务名称(按服务上下文解析)"},
		{Key: "GB_HOST_PORT", Value: VarHostPort, Description: "反向代理目标端口(按服务上下文解析)"},
		{Key: "GB_SUB_DOMAIN", Value: VarSubDomain, Description: "当前域名行的完整访问域名(<二级域名>.<根域名>,按域名行上下文解析)"},
		{Key: "GB_DATA_DIR", Value: dataDir, Description: "运行时数据根目录(路径原语)"},
		{Key: "GB_ACME_FILE", Value: acmeHome(dataDir), Description: "acme.sh 工作目录/证书根(= <GB_DATA_DIR>/tools/acme)"},
		{Key: "GB_SSL_FILE", Value: filepath.Join(VarACMEFile, "certs", VarSubDomain, "fullchain.pem"), Description: "当前域名行证书链路径(<GB_ACME_FILE>/certs/<GB_SUB_DOMAIN>/fullchain.pem)"},
		{Key: "GB_STATIC_ROOT", Value: staticRootDisplay, Description: "全局静态根目录(conf static_root)"},
		{
			Key:         "GB_LOG_FILE",
			Value:       filepath.Join(VarDataDir, "logs", "caddy", VarAppName+"_"+VarServiceName+".log"),
			Description: "按服务日志文件(<应用>_<服务>.log)",
		},
	}
}

// interpolateVars 把 s 中的 GateBox 变量替换为实际值:
//   - 内建最小变量:GB_APP / GB_SERVICE / GB_HOST_PORT / GB_DATA_DIR / GB_SUB_DOMAIN
//   - 组合变量:GB_ACME_FILE(<GB_DATA_DIR>/tools/acme)/ GB_SSL_FILE(当前域名行证书链)
//     / GB_STATIC_ROOT(全局静态根)/ GB_LOG_FILE(dataDir + 应用 + 服务)
//   - 用户变量:varEnv(API 层并入)
//   - 未命中的 <%...%> 视为配置错误(防拼错静默生成非法路径)。
//   - {$...}(caddy 环境变量占位符,可带默认 {$VAR:def})原样保留,交给 caddy 运行时替换。
func interpolateVars(s string, svc model.Service, appNames map[string]string, d model.ProxyDomain, varEnv map[string]string, staticRoot, dataDir string) (string, error) {
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
		case VarSubDomain:
			key = d.Host()
		case VarDataDir:
			key = dataDir
		case VarACMEFile:
			key = acmeHome(dataDir)
		case VarSSLFile:
			key = filepath.Join(acmeHome(dataDir), "certs", d.Host(), "fullchain.pem")
		case VarStaticRoot:
			if staticRoot != "" {
				key = staticRoot
			} else {
				key = m // 未配置全局静态根时保留,便于定位
			}
		case VarLogFile:
			key = ServiceLogPath(filepath.Join(dataDir, "logs", "caddy"), appName, svc.Name)
		default:
			if v, ok := varEnv[m]; ok {
				key = v
			} else if strings.HasPrefix(m, "<%GB_") {
				// 未定义的保留变量(全局变量),原样保留
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

// ServiceLogPath 计算内置「按服务日志」片段的日志文件路径(ADR-033):
// <logDir>/<app>_<service>.log;app 空回落 default,非法文件名字符清洗为 _。
func ServiceLogPath(logDir, appName, svcName string) string {
	if appName == "" {
		appName = "default"
	}
	return filepath.Join(logDir, sanitizeLogName(appName)+"_"+sanitizeLogName(svcName)+".log")
}

// sanitizeLogName 只保留 [A-Za-z0-9._-],其余替换为 _。
func sanitizeLogName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
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
func renderFragmentBody(code string, svc model.Service, appNames map[string]string, d model.ProxyDomain, varEnv map[string]string, staticRoot, dataDir string) ([]string, error) {
	interp, err := interpolateVars(code, svc, appNames, d, varEnv, staticRoot, dataDir)
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

// BuiltinFragmentCatalog 只读内置 Caddy 片段清单(ADR-018 修订 Q7 / ADR-033,7 个,不可改)。
// code 为真实片段体,遵循 caddy Caddyfile 语法,可含 <%VAR%>;
// 以 `reverse_proxy { ... }` 包裹者由生成器合并进受控反代块(ADR-033 §1)。
func BuiltinFragmentCatalog() []model.FragmentTemplate {
	return []model.FragmentTemplate{
		{
			ID:             model.FragmentGzip,
			Name:           "Gzip/Zstd 压缩",
			Description:    "对响应启用 gzip/zstd 压缩(标准版 Caddy 不含 brotli)",
			DefaultEnabled: true,
			Code:           "encode zstd gzip",
		},
		{
			ID:             model.FragmentBasicAuth,
			Name:           "Basic Auth 认证",
			Description:    "HTTP Basic 认证保护;默认账号 admin/admin,待「设置-用户」完善后改用用户库",
			DefaultEnabled: false,
			DefaultHidden:  true,
			Code:           "basic_auth {\n\tadmin $2a$14$ZqcXY/3dlZyPmCmX6yqRMucUD0GzD/doB4sk24ry8RQYOpcPslTDO\n}",
		},
		{
			// 可见但由生成器按 UpstreamProto=https 强制应用(前端显示为勾选且禁用)。
			ID:          model.FragmentSkipVerify,
			Name:        "忽略自带证书校验",
			Description: "反代到自签/内网 https 后端时跳过证书校验(后端协议=https 时自动启用)",
			Code:        "reverse_proxy {\n\ttransport http {\n\t\ttls_insecure_skip_verify\n\t}\n}",
		},
		{
			ID:             model.FragmentLogConfig,
			Name:           "按服务日志",
			Description:    "为该服务单独记录访问日志到 <dataDir>/logs/caddy/<应用>_<服务>.log",
			DefaultEnabled: true,
			Code:           "log {\n\toutput file <%GB_LOG_FILE%> {\n\t\troll_size 100mb\n\t\troll_keep 5\n\t}\n\tformat json\n}",
		},
		{
			ID:             model.FragmentBlockCommon,
			Name:           "阻止常见漏洞",
			Description:    "注入常见安全响应头并拒绝 TRACE/TRACK 方法",
			DefaultEnabled: true,
			Code:           "header {\n\t-X-Powered-By\n\tX-Content-Type-Options nosniff\n\tX-Frame-Options SAMEORIGIN\n\tReferrer-Policy strict-origin-when-cross-origin\n}\n@gb_blocked method TRACE TRACK\nrespond @gb_blocked 405",
		},
		{
			ID:             model.FragmentStaticCache,
			Name:           "静态资源缓存",
			Description:    "对常见静态资源扩展名设置 30 天强缓存",
			DefaultEnabled: false,
			Code:           "@gb_static path *.js *.css *.mjs *.png *.jpg *.jpeg *.gif *.svg *.ico *.webp *.avif *.woff *.woff2 *.ttf *.otf\nheader @gb_static Cache-Control \"public, max-age=2592000\"",
		},
		{
			ID:             model.FragmentWebsocket,
			Name:           "支持 websocket",
			Description:    "反向代理对 WebSocket/SSE 关闭响应缓冲(caddy 默认支持升级,此为流式优化)",
			DefaultEnabled: true,
			Code:           "reverse_proxy {\n\tflush_interval -1\n}",
		},
	}
}

// DefaultEnabledFragmentIDs 返回应默认启用(默认勾选且非隐藏)的内置/片段 ID。
// 由 API 层在创建 Service 时 seed 进 FragmentIDs;含 hidden 的强加片段也在此。
func DefaultEnabledFragmentIDs(fragments []model.Fragment, builtins []model.FragmentTemplate) []string {
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
