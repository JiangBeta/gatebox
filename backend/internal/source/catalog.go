package source

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Variant 核心组件配方变体（与 GateBoxStore index.json 的 variants[] 对应）。
type Variant struct {
	Key       string   `json:"key"`
	Component string   `json:"component"`
	Version   string   `json:"version"`
	Features  []string `json:"features"`
	OS        string   `json:"os"`
	Arch      string   `json:"arch"`
	URL       string   `json:"url"`
	SHA256    string   `json:"sha256"`
	Size      int64    `json:"size"`
}

// Catalog 静态索引（catalog v1）中与变体相关的部分。
type Catalog struct {
	Schema   string    `json:"schema"`
	Variants []Variant `json:"variants"`
}

// FetchCatalog 拉取并解析静态索引（catalog v1）。
func (c *Client) FetchCatalog(ctx context.Context, indexURL string) (*Catalog, error) {
	data, err := c.Get(ctx, indexURL)
	if err != nil {
		return nil, err
	}
	var cat Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return nil, err
	}
	return &cat, nil
}

// VariantByKey 按变体键查找变体。
func (cat *Catalog) VariantByKey(key string) (Variant, bool) {
	for _, v := range cat.Variants {
		if v.Key == key {
			return v, true
		}
	}
	return Variant{}, false
}

// InstallVersioned 按变体键把制品落到 versions/<key>/ 并更新 active 二进制，
// 保留历史版本以支持回滚（ADR-037 §4 / ADR-038 §7）。
func InstallVersioned(dataDir, id, key string, data []byte) (string, error) {
	dir := filepath.Join(dataDir, "tools", id)
	verDir := filepath.Join(dir, "versions", safeKey(key))
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(verDir, id), data, 0o755); err != nil {
		return "", err
	}
	active := filepath.Join(dir, id)
	if err := InstallAtomic(active, data); err != nil {
		return "", err
	}
	return active, nil
}

// safeKey 把变体键转为安全目录名（冒号等换为连字符）。
func safeKey(key string) string {
	r := strings.NewReplacer(":", "-", "/", "-", "\\", "-", " ", "-")
	return r.Replace(strings.TrimSpace(key))
}
