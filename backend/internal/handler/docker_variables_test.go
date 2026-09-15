package handler

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/JiangBeta/gatebox/internal/adapter/docker/compose"
	"github.com/JiangBeta/gatebox/internal/repository"
)

// TestInterpolateContainerVarsBuiltins GB_PROJ_FILE/GB_DATA_DIR/GB_PROJ_NAME 统一来源。
func TestInterpolateContainerVarsBuiltins(t *testing.T) {
	dataDir := t.TempDir()
	s, err := repository.Open(dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	d := &dockerAPI{dataDir: dataDir, cmp: compose.New(dataDir), s: s}

	yaml := "services:\n  a:\n    volumes:\n      - ${GB_PROJ_FILE}/data:/data\n    environment:\n      D: ${GB_DATA_DIR}\n      N: ${GB_PROJ_NAME}\n"
	out, err := d.interpolateContainerVars("myproj", "", yaml)
	if err != nil {
		t.Fatalf("interpolate: %v", err)
	}
	wantFile := filepath.Join(dataDir, "appData", "myproj")
	if !strings.Contains(out, wantFile+"/data:/data") {
		t.Errorf("GB_PROJ_FILE 未解析为 %q:\n%s", wantFile, out)
	}
	if !strings.Contains(out, "D: "+dataDir) {
		t.Errorf("GB_DATA_DIR 未解析为 %q:\n%s", dataDir, out)
	}
	if !strings.Contains(out, "N: myproj") {
		t.Errorf("GB_PROJ_NAME 未解析:\n%s", out)
	}
}

// TestContainerSystemVariables 系统变量描述符含 GB_DATA_DIR,GB_PROJ_FILE 公式一致。
func TestContainerSystemVariables(t *testing.T) {
	vs := containerSystemVariables("/data/root")
	byKey := map[string]systemVariableView{}
	for _, v := range vs {
		byKey[v.Key] = v
	}
	data, ok := byKey[ProjectVarData]
	if !ok || data.Value != "/data/root" || data.Context != "container" {
		t.Errorf("GB_DATA_DIR 描述符错误: %+v", data)
	}
	file, ok := byKey[ProjectVarFile]
	if !ok || !strings.Contains(file.Description, "<GB_DATA_DIR>/appData/<GB_PROJ_NAME>") || !strings.Contains(file.Description, "不含结尾 /") {
		t.Errorf("GB_PROJ_FILE 描述应含统一公式与「不含结尾 /」: %+v", file)
	}
	if v, ok := byKey[ACMEFileVar]; !ok || v.Value != filepath.Join("/data/root", "tools", "acme") {
		t.Errorf("GB_ACME_FILE 描述符错误: %+v", v)
	}
	if _, ok := byKey[SubDomainVar]; !ok {
		t.Errorf("缺 GB_SUB_DOMAIN 描述符")
	}
	if _, ok := byKey[SSLFileVar]; !ok {
		t.Errorf("缺 GB_SSL_FILE 描述符")
	}
}

// TestInterpolateContainerVarsACME:GB_SUB_DOMAIN 取首个 caddy 站点域名,
// GB_ACME_FILE/GB_SSL_FILE 据此解析;无站点时 GB_SUB_DOMAIN 不注入。
func TestInterpolateContainerVarsACME(t *testing.T) {
	dataDir := t.TempDir()
	s, err := repository.Open(dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	d := &dockerAPI{dataDir: dataDir, cmp: compose.New(dataDir), s: s}

	yaml := "services:\n  a:\n    image: nginx\n    labels:\n      caddy: app.example.com\n    environment:\n      S: ${GB_SUB_DOMAIN}\n      A: ${GB_ACME_FILE}\n      C: ${GB_SSL_FILE}\n"
	out, err := d.interpolateContainerVars("myproj", "", yaml)
	if err != nil {
		t.Fatalf("interpolate: %v", err)
	}
	acmeHome := filepath.Join(dataDir, "tools", "acme")
	if !strings.Contains(out, "S: app.example.com") {
		t.Errorf("GB_SUB_DOMAIN 未解析:\n%s", out)
	}
	if !strings.Contains(out, "A: "+acmeHome) {
		t.Errorf("GB_ACME_FILE 未解析为 %q:\n%s", acmeHome, out)
	}
	wantSSL := filepath.Join(acmeHome, "certs", "app.example.com", "fullchain.pem")
	if !strings.Contains(out, "C: "+wantSSL) {
		t.Errorf("GB_SSL_FILE 未解析为 %q:\n%s", wantSSL, out)
	}

	noSite := "services:\n  a:\n    image: nginx\n    environment:\n      S: ${GB_SUB_DOMAIN}\n"
	out2, err := d.interpolateContainerVars("myproj", "", noSite)
	if err != nil {
		t.Fatalf("interpolate: %v", err)
	}
	if !strings.Contains(out2, "S: ${GB_SUB_DOMAIN}") {
		t.Errorf("无 caddy 站点时 GB_SUB_DOMAIN 应原样保留:\n%s", out2)
	}
}
