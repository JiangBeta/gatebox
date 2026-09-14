package api

// 网关端口设置(conf 持久化,重启生效)——ADR-026 多 https 端口。
//
// gatebox.conf 是行式 `key = value`;读写只 upsert 对应键,保留其它行与注释。

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// gatewaySettings 网关端口相关设置(读取现行 conf,不含进程内覆盖)。
type gatewaySettings struct {
	CaddyHTTPPort        int    `json:"caddyHTTPPort"`
	CaddyHTTPSPort       int    `json:"caddyHTTPSPort"`
	CaddyHTTPSExtraPorts []int  `json:"caddyHTTPSExtraPorts"`
	AcmeBin              string `json:"acmeBin,omitempty"`
	ConfFile             string `json:"confFile"`
}

// confPath 返回 gatebox.conf 绝对路径(以 dataDir 定位)。
func (a *gatewayAPI) confPath() string {
	return filepath.Join(a.dataDir, "conf", "gatebox.conf")
}

// getGatewaySettings 返回当前 conf 中的网关端口设置。
func (a *gatewayAPI) getGatewaySettings(w http.ResponseWriter, r *http.Request) {
	vals := readConfKV(a.confPath())
	settings := gatewaySettings{ConfFile: a.confPath()}
	settings.CaddyHTTPPort = confInt(vals, "caddy_http_port")
	settings.CaddyHTTPSPort = confInt(vals, "caddy_https_port")
	settings.CaddyHTTPSExtraPorts = confIntList(vals, "caddy_https_extra_ports")
	settings.AcmeBin = vals["acme_bin"]
	writeJSON(w, http.StatusOK, settings)
}

// putGatewaySettings 写入 conf 的网关端口设置(重启后生效)。
func (a *gatewayAPI) putGatewaySettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CaddyHTTPPort        int   `json:"caddyHTTPPort"`
		CaddyHTTPSPort       int   `json:"caddyHTTPSPort"`
		CaddyHTTPSExtraPorts []int `json:"caddyHTTPSExtraPorts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if in.CaddyHTTPPort < 0 || in.CaddyHTTPSPort < 0 || in.CaddyHTTPPort > 65535 || in.CaddyHTTPSPort > 65535 {
		writeErr(w, http.StatusBadRequest, "端口超出范围(0~65535)")
		return
	}
	for _, p := range in.CaddyHTTPSExtraPorts {
		if p <= 0 || p > 65535 {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("额外端口非法: %d", p))
			return
		}
	}
	var extra strings.Builder
	for i, p := range in.CaddyHTTPSExtraPorts {
		if i > 0 {
			extra.WriteByte(',')
		}
		extra.WriteString(strconv.Itoa(p))
	}
	kv := map[string]string{
		"caddy_http_port":         strconv.Itoa(in.CaddyHTTPPort),
		"caddy_https_port":        strconv.Itoa(in.CaddyHTTPSPort),
		"caddy_https_extra_ports": extra.String(),
	}
	if err := upsertConf(a.confPath(), kv); err != nil {
		writeErr(w, http.StatusInternalServerError, "写入配置失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "note": "需要重启服务后生效"})
}

// readConfKV 读 gatebox.conf 到 key→value(非法行忽略)。
func readConfKV(path string) map[string]string {
	vals := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return vals
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		i := strings.Index(raw, "=")
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(raw[:i])
		val := strings.TrimSpace(raw[i+1:])
		vals[key] = val
	}
	return vals
}

// confInt 从 conf 取值转 int;缺省/非法返回 0。
func confInt(vals map[string]string, key string) int {
	if v, ok := vals[key]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 && n <= 65535 {
			return n
		}
	}
	return 0
}

// confIntList 从逗号分隔端口串解析额外监听端口列表。
func confIntList(vals map[string]string, key string) []int {
	v := strings.TrimSpace(vals[key])
	if v == "" {
		return nil
	}
	var out []int
	for _, part := range strings.Split(v, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if n, err := strconv.Atoi(p); err == nil && n > 0 && n <= 65535 {
			out = append(out, n)
		}
	}
	return out
}

// upsertConf 把 kv 写回 conf 文件:已有键替换该行,缺失键追加;其余行与注释保留。
func upsertConf(path string, kv map[string]string) error {
	f, err := os.Open(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var out []string
	present := map[string]bool{}
	if f != nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			trimmed := strings.TrimSpace(line)
			key := ""
			if !strings.HasPrefix(trimmed, "#") {
				if i := strings.Index(trimmed, "="); i > 0 {
					key = strings.TrimSpace(trimmed[:i])
				}
			}
			if v, ok := kv[key]; ok {
				out = append(out, key+" = "+v)
				present[key] = true
				continue
			}
			out = append(out, line)
		}
		f.Close()
	}
	// 追加缺失键(保证确定性顺序)
	var missing []string
	for k := range kv {
		if !present[k] {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	for _, k := range missing {
		out = append(out, k+" = "+kv[k])
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o644)
}
