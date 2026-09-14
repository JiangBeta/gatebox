// Package source 提供制品源：GitHub Release / 静态索引解析、下载、校验、解包与原子安装。
//
// 设计见 docs/architecture.md §5 与 ADR-027 / ADR-029。
package source

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Asset 一个可下载制品。
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
	// SHA256 静态索引可提供校验和；GitHub 源通常为空。
	SHA256 string `json:"sha256,omitempty"`
}

// Release 某源上的一个发布。
type Release struct {
	Version string  `json:"version"`
	Assets  []Asset `json:"assets"`
}

// Client HTTP 客户端（带超时）。
type Client struct {
	HTTP *http.Client
	// Token 可选 GitHub token（避免限流）。
	Token string
}

// New 构造制品源客户端。
func New(token string) *Client {
	return &Client{HTTP: &http.Client{Timeout: 60 * time.Second}, Token: token}
}

var versionRE = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

// ParseVersion 从任意字符串提取首个语义版本号。
func ParseVersion(s string) string {
	return versionRE.FindString(s)
}

// CompareVersions 比较版本：a>b 返回 1，a<b 返回 -1，相等 0。
// 非数字段按字典序比较；缺失段按 0 处理。
func CompareVersions(a, b string) int {
	pa, pb := splitVer(a), splitVer(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		va, vb := 0, 0
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}
		if va != vb {
			if va > vb {
				return 1
			}
			return -1
		}
	}
	return 0
}

func splitVer(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}

// GitHubLatest 查询 GitHub 仓库最新 release。
func (c *Client) GitHubLatest(ctx context.Context, repo string) (Release, error) {
	url := "https://api.github.com/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub 查询失败: HTTP %d", resp.StatusCode)
	}
	var raw struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Release{}, err
	}
	rel := Release{Version: ParseVersion(raw.TagName)}
	for _, a := range raw.Assets {
		rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: a.URL, Size: a.Size})
	}
	return rel, nil
}

// Index 静态索引中的一条记录（ADR-029）。
type Index struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	Version  string  `json:"version"`
	Channel  string  `json:"channel"`
	Artifact Asset   `json:"artifact"`
	Extra    []Asset `json:"extraAssets,omitempty"`
}

// FetchIndex 拉取静态索引并解析。
func (c *Client) FetchIndex(ctx context.Context, indexURL string) ([]Index, error) {
	if strings.TrimSpace(indexURL) == "" {
		return nil, errors.New("未配置索引地址")
	}
	body, err := c.Get(ctx, indexURL)
	if err != nil {
		return nil, err
	}
	// 兼容 {"items":[...]} 与裸数组。
	var arr []Index
	if err := json.Unmarshal(body, &arr); err != nil {
		var wrap struct {
			Items []Index `json:"items"`
		}
		if err2 := json.Unmarshal(body, &wrap); err2 != nil {
			return nil, fmt.Errorf("解析索引失败: %w", err)
		}
		arr = wrap.Items
	}
	return arr, nil
}

// Get 下载 URL 内容。
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if c.Token != "" && strings.Contains(url, "github.com") {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 512<<20))
}

// PickAsset 依据当前 OS/Arch 从发布资产中挑选最匹配的制品。
func PickAsset(assets []Asset) (Asset, bool) {
	arch := []string{"amd64", "x86_64", "x64"}
	if runtime.GOARCH == "arm64" {
		arch = []string{"arm64", "aarch64"}
	}
	type scored struct {
		a     Asset
		score int
	}
	var cand []scored
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		score := 0
		if strings.Contains(name, "linux") {
			score += 4
		}
		for _, tk := range arch {
			if strings.Contains(name, tk) {
				score += 3
				break
			}
		}
		switch {
		case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
			score += 2
		case strings.HasSuffix(name, ".zip"):
			score += 1
		case !strings.Contains(name, "."):
			score += 2 // 裸二进制
		}
		if strings.Contains(name, "sha256") || strings.Contains(name, ".sig") || strings.Contains(name, ".txt") {
			score -= 10
		}
		if score > 0 {
			cand = append(cand, scored{a, score})
		}
	}
	if len(cand) == 0 {
		return Asset{}, false
	}
	sort.SliceStable(cand, func(i, j int) bool { return cand[i].score > cand[j].score })
	return cand[0].a, true
}

// VerifySHA256 校验数据摘要；wantSHA 为空则跳过。
func VerifySHA256(data []byte, wantSHA string) error {
	if strings.TrimSpace(wantSHA) == "" {
		return nil
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, strings.TrimSpace(wantSHA)) {
		return fmt.Errorf("校验和不匹配: got %s", got[:12])
	}
	return nil
}

// ExtractBinary 从 tar.gz / zip / 裸二进制中提取可执行文件。
// nameHint 用于优先挑选文件名包含该串的成员。
func ExtractBinary(data []byte, nameHint string) ([]byte, error) {
	switch {
	case len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b: // gzip
		return extractTarGz(data, nameHint)
	case len(data) > 4 && string(data[:4]) == "PK\x03\x04": // zip
		return extractZip(data, nameHint)
	default:
		return data, nil
	}
}

func extractTarGz(data []byte, hint string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var best []byte
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg || h.Size == 0 {
			continue
		}
		if !strings.HasPrefix(h.Name, ".") && !strings.Contains(h.Name, "/.") && !strings.HasSuffix(h.Name, "/") {
			b, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			if best == nil {
				best = b
			}
			if hint != "" && strings.Contains(filepath.Base(h.Name), hint) {
				return b, nil
			}
		}
	}
	if best == nil {
		return nil, errors.New("压缩包内未找到可执行文件")
	}
	return best, nil
}

func extractZip(data []byte, hint string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var best []byte
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		if best == nil {
			best = b
		}
		if hint != "" && strings.Contains(filepath.Base(f.Name), hint) {
			return b, nil
		}
	}
	if best == nil {
		return nil, errors.New("压缩包内未找到可执行文件")
	}
	return best, nil
}

// InstallAtomic 原子替换目标文件：先写临时文件，再 rename；原文件备份为 .bak。
func InstallAtomic(dest string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(dest); err == nil {
		_ = os.Rename(dest, dest+".bak")
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
