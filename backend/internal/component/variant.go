package component

import (
	"context"
	"fmt"
	"runtime"

	"github.com/JiangBeta/gatebox/internal/extension"
	"github.com/JiangBeta/gatebox/internal/source"
)

// ApplyVariant 按「组件版本 + 特征并集」解析配方变体并安装（ADR-038）。
//
// 步骤：算变体键 → 拉静态索引找变体 → 下载校验 → 版本化安装（保留历史供回滚）。
// 内核不本地编译、不调用无 SLA 的上游构建服务；制品的下载与替换由本方法完成，
// 组件重启由调用方在成功后触发。
func (r *CoreRegistry) ApplyVariant(ctx context.Context, id, version string, features []string, indexURL string) (string, error) {
	key := extension.VariantKey(id, version, "linux", archOf(), features)
	cat, err := r.src.FetchCatalog(ctx, indexURL)
	if err != nil {
		return "", fmt.Errorf("拉取索引失败: %w", err)
	}
	v, ok := cat.VariantByKey(key)
	if !ok {
		return "", fmt.Errorf("未找到匹配变体 %s（可在 GateBoxStore 触发按需构建）", key)
	}
	data, err := r.src.Get(ctx, v.URL)
	if err != nil {
		return "", fmt.Errorf("下载变体失败: %w", err)
	}
	if err := source.VerifySHA256(data, v.SHA256); err != nil {
		return "", err
	}
	bin, err := source.ExtractBinary(data, id)
	if err != nil {
		return "", err
	}
	return source.InstallVersioned(r.dataDir, id, key, bin)
}

func archOf() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}
