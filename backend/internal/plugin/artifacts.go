package plugin

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/JiangBeta/gatebox/internal/source"
)

// pickRole 选择当前平台的指定 role 制品（空 role 视为 binary，兼容旧 manifest）。
func pickRole(man Manifest, role string) (Artifact, bool) {
	for _, a := range man.Artifacts {
		r := a.Role
		if r == "" {
			r = RoleBinary
		}
		if r != role {
			continue
		}
		if a.OS != "" && a.OS != "linux" {
			continue
		}
		if a.Arch != "" && a.Arch != arch() {
			continue
		}
		if a.URL != "" {
			return a, true
		}
	}
	return Artifact{}, false
}

// artifactDest 计算制品落盘路径（install.to 优先，否则按 role 约定）。
func (m *Manager) artifactDest(man Manifest, a Artifact) string {
	if to := strings.TrimSpace(a.Install.To); to != "" {
		return filepath.Join(m.dataDir, filepath.Clean("/"+to))
	}
	switch a.Role {
	case RoleUI:
		return filepath.Join(m.dataDir, "tools", man.ID, "ui")
	case RoleAssets:
		return filepath.Join(m.dataDir, "tools", man.ID, "assets")
	case RoleSidecar:
		// 侧车与「插件提供的组件二进制」区分命名，避免互相覆盖。
		return filepath.Join(m.dataDir, "tools", man.ID, man.ID+"-sidecar")
	default:
		return filepath.Join(m.dataDir, "tools", man.ID, man.ID)
	}
}

// installArtifacts 下载并安装 manifest 声明的全部制品（binary/sidecar/ui/assets）。
//
// 每个制品校验 sha256；单文件直接原子安装，归档解包到目标目录（ADR-037/039）。
func (m *Manager) installArtifacts(ctx context.Context, man Manifest) error {
	for _, a := range man.Artifacts {
		if a.URL == "" {
			continue
		}
		if a.OS != "" && a.OS != "linux" {
			continue
		}
		if a.Arch != "" && a.Arch != arch() {
			continue
		}
		data, err := m.src.Get(ctx, a.URL)
		if err != nil {
			return fmt.Errorf("下载制品 %s 失败: %w", a.Role, err)
		}
		if err := source.VerifySHA256(data, a.SHA256); err != nil {
			return err
		}
		dest := m.artifactDest(man, a)
		switch a.Role {
		case RoleUI, RoleAssets:
			if err := extractArchive(data, dest); err != nil {
				return fmt.Errorf("解包 %s 失败: %w", a.Role, err)
			}
		default:
			bin, err := source.ExtractBinary(data, man.ID)
			if err != nil {
				return err
			}
			if err := source.InstallAtomic(dest, bin); err != nil {
				return err
			}
		}
	}
	return nil
}

// extractArchive 把 tar.gz / zip 解包到 destDir（防路径穿越）；非归档则按单文件写入。
func extractArchive(data []byte, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	switch {
	case len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b:
		return extractTarGz(data, destDir)
	case len(data) > 4 && bytes.Equal(data[:4], []byte("PK\x03\x04")):
		return extractZip(data, destDir)
	default:
		return os.WriteFile(filepath.Join(destDir, "data"), data, 0o644)
	}
}

func safeJoin(destDir, name string) (string, error) {
	clean := filepath.Clean("/" + name)
	fp := filepath.Join(destDir, clean)
	if !strings.HasPrefix(fp, filepath.Clean(destDir)+string(os.PathSeparator)) && fp != filepath.Clean(destDir) {
		return "", fmt.Errorf("非法路径: %s", name)
	}
	return fp, nil
}

func extractTarGz(data []byte, destDir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		fp, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(fp, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(fp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
}

func extractZip(data []byte, destDir string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		fp, err := safeJoin(destDir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fp, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(fp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode()&0o777)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}
	return nil
}
