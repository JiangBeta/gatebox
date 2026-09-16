package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/JiangBeta/gatebox/internal/extension"
)

// 配方变体「按需构建」：变体矩阵未命中时，触发 GateBoxStore 的构建 workflow（ADR-038 §4）。
//
// 配置经 SetVariantBuild 注入（启动时一次）；无令牌时退化为返回手动触发链接。

var variantBuild = struct {
	repo     string
	workflow string
	token    string
}{}

// SetVariantBuild 注入变体构建目标（main 启动时调用）。
func SetVariantBuild(repo, workflow, token string) {
	variantBuild.repo = repo
	variantBuild.workflow = workflow
	variantBuild.token = token
}

// variantInfo 变体解析信息（供前端展示与触发构建）。
type variantInfo struct {
	Component string   `json:"component"`
	Version   string   `json:"version"`
	Features  []string `json:"features"`
	Key       string   `json:"key"`
	BuildURL  string   `json:"buildUrl"`
	Runnable  bool     `json:"runnable"` // 是否可自动派发（已配置令牌）
}

func variantKey(component, version string, features []string) string {
	return extension.VariantKey(component, version, "linux", "amd64", features)
}

// variantBuildURL 返回 GitHub Actions 手动触发页。
func variantBuildURL() string {
	if variantBuild.repo == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/actions/workflows/%s", variantBuild.repo, variantBuild.workflow)
}

// dispatchVariantBuild 调用 GitHub workflow_dispatch 触发变体构建。
func dispatchVariantBuild(ctx context.Context, version, features string) error {
	if variantBuild.token == "" || variantBuild.repo == "" || variantBuild.workflow == "" {
		return fmt.Errorf("未配置 GitHub 令牌，无法自动派发")
	}
	body, _ := json.Marshal(map[string]any{
		"ref": "main",
		"inputs": map[string]string{
			"version": version, "features": features, "os": "linux", "arch": "amd64",
		},
	})
	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/workflows/%s/dispatches", variantBuild.repo, variantBuild.workflow)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+variantBuild.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GitHub 派发失败: HTTP %d", resp.StatusCode)
	}
	return nil
}
