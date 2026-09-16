package source

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// VerifyEd25519 校验 detached Ed25519 签名（base64 编码）。
//
// 用于静态索引（index.json）与制品的发布者签名校验（ADR-037 §本阶段实现边界）。
func VerifyEd25519(data []byte, sigB64, pubB64 string) error {
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubB64))
	if err != nil {
		return fmt.Errorf("公钥 base64 非法: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("公钥长度非法: %d", len(pub))
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil {
		return fmt.Errorf("签名 base64 非法: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), data, sig) {
		return errors.New("签名校验失败")
	}
	return nil
}

// FetchCatalogSigned 拉取静态索引并在配置了公钥时校验 detached 签名（<indexURL>.sig）。
func (c *Client) FetchCatalogSigned(ctx context.Context, indexURL, pubB64 string) (*Catalog, error) {
	data, err := c.Get(ctx, indexURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(pubB64) != "" {
		sig, err := c.Get(ctx, indexURL+".sig")
		if err != nil {
			return nil, fmt.Errorf("拉取索引签名失败: %w", err)
		}
		if err := VerifyEd25519(data, string(sig), pubB64); err != nil {
			return nil, err
		}
	}
	var cat Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return nil, err
	}
	return &cat, nil
}
