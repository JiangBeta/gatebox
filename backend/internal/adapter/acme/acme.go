// Package acme 封装 acme.sh CLI,负责 DNS-01 证书签发与落盘。
//
// 证书全部落到 <CertsDir>/<fqdn>/ 下,文件约定:
//
//	fullchain.pem  -- 完整链路(leaf + 中间)
//	key.pem        -- 私钥
//
// caddy 侧通过 tls <fullchain> <key> 引用这些文件(ADR-013 修订)。
package acme

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JiangBeta/gatebox/internal/model"
)

// runCmd 执行 acme.sh;包级变量便于测试注入。
var runCmd = run

// CertFile 单个 fqdn 的证书产物路径。
type CertFile struct {
	FQDN      string
	Cert      string // fullchain.pem
	Key       string // key.pem
	Dir       string
	NotBefore time.Time
	NotAfter  time.Time
}

// Issuer 通过 acme.sh 签发并安装证书。
type Issuer struct {
	CertsDir string // 证书根目录,如 data/tools/acme/certs
	// HomeDir acme.sh 工作目录(--home),所有账号/证书缓存都在此目录内完成,
	// 不经过 ~/.acme.sh。默认取 CertsDir 的上一级(即 <dataDir>/tools/acme)。
	HomeDir string
	BinPath string // acme.sh 可执行文件路径,默认 "acme.sh"(PATH)

	mu          sync.Mutex
	failCooldow map[string]time.Time // fqdn → 下次可重试时间(签发失败后冷却)

	email        string // ACME 注册邮箱(全局,空=不指定)
	accountReady bool   // 当前 email 是否已注册过账号(避免每次签发重复注册)
	accountEmail string // accountReady 对应的邮箱
}

// 签发失败冷却期:避免每次 reloadCaddy 都反复调用 acme.sh 打同一坏域名
// (例如凭证失效时每次操作等 8-9s×域名数)。冷却期内直接跳过,不影响其余操作。
const failCooldown = 10 * time.Minute

// dnsSleepSeconds acme.sh --dnssleep:添加 TXT 后固定等待的秒数(替代默认 20s 的
// 公开 DNS 检查)。DNSPod 双权威 NS 集群同步存在延迟,等待不足时 Let's Encrypt
// 多视角二次校验会报 "secondary validation: No TXT record found"。
const dnsSleepSeconds = 300

// issueAttempts 单次 ensure 最多尝试签发次数(含首次),即首次失败后重试 2 次。
const issueAttempts = 3

// retryableIssueError 判断签发失败是否为 DNS 传播类瞬态错误(值得重试);
// 凭证错误(401 等)不匹配 → 不重试,避免无谓等待。
func retryableIssueError(out string) bool {
	for _, s := range []string{
		"Invalid status",
		"No TXT record found",
		"secondary validation",
		"Verification error",
		"timeout",
	} {
		if strings.Contains(out, s) {
			return true
		}
	}
	return false
}

// issueArgs 构造 acme.sh 签发参数(--home + --issue + DNS-01 + dnssleep)。
func (i *Issuer) issueArgs(params, fqdn string, force bool) []string {
	args := i.homeArgs()
	args = append(args, "--issue", "--dns", params, "-d", fqdn,
		"--keylength", "ec-256", "--server", "letsencrypt",
		"--dnssleep", strconv.Itoa(dnsSleepSeconds))
	if force {
		args = append(args, "--force")
	}
	return args
}

// New 构造 Issuer。
func New(certsDir, binPath string) *Issuer {
	if binPath == "" {
		// 未显式配置时:优先组件安装路径 <dataDir>/tools/acme/acme.sh(= certsDir 的上一级),
		// 否则回退 PATH 中的 "acme.sh"。
		cand := filepath.Join(filepath.Dir(certsDir), "acme.sh")
		if _, err := os.Stat(cand); err == nil {
			binPath = cand
		} else {
			binPath = "acme.sh"
		}
	}
	return &Issuer{CertsDir: certsDir, HomeDir: filepath.Dir(certsDir), BinPath: binPath, failCooldow: map[string]time.Time{}}
}

// homeArgs 返回 acme.sh 的 --home 参数,确保账号/证书全部落在 HomeDir 内
// (默认 <dataDir>/tools/acme),不读也不写 ~/.acme.sh。
func (i *Issuer) homeArgs() []string {
	if i.HomeDir == "" {
		return nil
	}
	return []string{"--home", i.HomeDir}
}

// SetEmail 设置全局 ACME 注册邮箱;变更后下次签发会重新注册账号。
func (i *Issuer) SetEmail(email string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.email != email {
		i.email = email
		i.accountReady = false
	}
}

// Email 返回当前全局 ACME 注册邮箱(空=未设置)。
func (i *Issuer) Email() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.email
}

// emailEnv 把注册邮箱以 acme.sh 识别的 ACCOUNT_EMAIL 环境变量下发。
func (i *Issuer) emailEnv() []string {
	if e := i.Email(); e != "" {
		return []string{"ACCOUNT_EMAIL=" + e}
	}
	return nil
}

// ensureAccount 在需要时用邮箱注册一次 ACME 账号(幂等:同邮箱仅注册一次)。
// 邮箱为空时直接返回;注册成功后缓存,避免每个域名重复请求。
func (i *Issuer) ensureAccount(ctx context.Context, appendLog func(string)) error {
	email := i.Email()
	if email == "" {
		return nil
	}
	i.mu.Lock()
	ready := i.accountReady && i.accountEmail == email
	i.mu.Unlock()
	if ready {
		return nil
	}
	args := append(i.homeArgs(), "--register-account", "-m", email, "--server", "letsencrypt")
	out, err := runCmd(ctx, i.BinPath, i.emailEnv(), args...)
	if appendLog != nil {
		appendLog(out)
	}
	if err != nil {
		return fmt.Errorf("acme.sh 注册账号失败: %w", err)
	}
	i.mu.Lock()
	i.accountReady = true
	i.accountEmail = email
	i.mu.Unlock()
	return nil
}

// CertsDirOf 返回某个 fqdn 的证书目录。
func (i *Issuer) CertsDirOf(fqdn string) string {
	return filepath.Join(i.CertsDir, fqdn)
}

// Paths 返回某个 fqdn 的证书文件路径(未要求存在)。
func (i *Issuer) Paths(fqdn string) CertFile {
	dir := i.CertsDirOf(fqdn)
	return CertFile{
		FQDN: fqdn,
		Cert: filepath.Join(dir, "fullchain.pem"),
		Key:  filepath.Join(dir, "key.pem"),
		Dir:  dir,
	}
}

// Exists 返回证书是否已落盘。
func (i *Issuer) Exists(fqdn string) bool {
	p := i.Paths(fqdn)
	_, cerr := os.Stat(p.Cert)
	_, kerr := os.Stat(p.Key)
	return cerr == nil && kerr == nil
}

// parseCertFile 解析证书 PEM 返回有效期。
func parseCertFile(path string) (time.Time, time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return time.Time{}, time.Time{}, errors.New("无效 PEM: " + path)
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return c.NotBefore, c.NotAfter, nil
}

// Ensure 确保证书存在且有效;否则通过 acme.sh 签发(DNS-01)。
// 证书在续期窗口内(有效期剩余 > 30 天)视为有效,不重复签发。
// 返回本次 acme.sh 原始输出日志文件路径(SSL 页「日志」查看用)。
func (i *Issuer) Ensure(ctx context.Context, cred model.DNSCredential, fqdn string) (string, error) {
	return i.ensure(ctx, cred, fqdn, false)
}

// ForceRenew 强制重新申请证书(忽略有效性与失败冷却)。供证书管理「重新申请」。
func (i *Issuer) ForceRenew(ctx context.Context, cred model.DNSCredential, fqdn string) (string, error) {
	return i.ensure(ctx, cred, fqdn, true)
}

// Remove 删除某个 fqdn 的证书文件(DNS 管理「删除证书」)。
func (i *Issuer) Remove(fqdn string) error {
	return os.RemoveAll(i.CertsDirOf(fqdn))
}

// ensure 核心签发逻辑。force=true 时忽略有效性判断与冷却,强制执行 --force 重签。
// 返回 acme.sh 原始输出日志文件路径(可能为空)。
func (i *Issuer) ensure(ctx context.Context, cred model.DNSCredential, fqdn string, force bool) (string, error) {
	p := i.Paths(fqdn)
	if !force {
		if _, na, err := parseCertFile(p.Cert); err == nil {
			if time.Until(na) > 30*24*time.Hour {
				return "", nil
			}
		}
		// 冷却:失败过的域名短期内不重试(凭证坏 / DNS 未生效等),避免反复阻塞 reload。
		// 注意:判断与解锁必须一次性完成,不能在分支里先解锁再走第二个 Unlock
		// (曾导致 fatal error: sync: unlock of unlocked mutex)。
		i.mu.Lock()
		until, cooled := i.failCooldow[fqdn]
		i.mu.Unlock()
		if cooled && time.Now().Before(until) {
			return "", nil
		}
	}

	params, env, err := providerEnv(cred)
	if err != nil {
		return "", err
	}
	env = append(append([]string{}, env...), i.emailEnv()...)
	if err := os.MkdirAll(p.Dir, 0o750); err != nil {
		return "", err
	}

	logFile, err := i.writeIssueLog(fqdn)
	if err != nil {
		// 日志文件写失败不阻塞签发(仅告警)
		logFile = ""
	}
	// 追加一段 acme.sh 原始输出到日志文件
	appendLog := func(out string) {
		if logFile == "" || out == "" {
			return
		}
		f, ferr := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
		if ferr != nil {
			return
		}
		defer f.Close()
		_, _ = f.Write([]byte(out))
	}

	// 账号注册:配置了邮箱时先确保账号已注册(幂等)。所有操作都在 HomeDir 内。
	if err := i.ensureAccount(ctx, appendLog); err != nil {
		if !force {
			i.mu.Lock()
			i.failCooldow[fqdn] = time.Now().Add(failCooldown)
			i.mu.Unlock()
		}
		return logFile, err
	}

	// 签发:acme.sh --issue --dns <params> -d <fqdn> --keylength ec-256 --server letsencrypt
	// 用 Let's Encrypt 作默认 CA(ZeroSSL 需先注册 EAB,不适合无交互场景)。
	// --dnssleep 等待 DNS 传播(DNSPod 二次校验需要);force 加 --force。
	// 瞬态失败(如二次校验 No TXT record found)最多尝试 issueAttempts 次,重试时
	// 追加 --force 强制新建订单,避免复用已失效订单。
	issueEnv := envMap(env)
	baseArgs := i.issueArgs(params, fqdn, force)
	var issueOut string
	var issueErr error
	for attempt := 0; attempt < issueAttempts; attempt++ {
		attemptArgs := baseArgs
		if attempt > 0 {
			attemptArgs = append(append([]string{}, baseArgs...), "--force")
		}
		issueOut, issueErr = runCmd(ctx, i.BinPath, issueEnv, attemptArgs...)
		appendLog(issueOut)
		if issueErr == nil || strings.Contains(issueErr.Error(), "Domains not changed") {
			break
		}
		if retryableIssueError(issueOut) && attempt < issueAttempts-1 {
			continue
		}
		break
	}
	if issueErr != nil && !strings.Contains(issueErr.Error(), "Domains not changed") {
		// "Domains not changed":acme.sh 侧证书仍有效、跳过重签(exit 2),视为成功,
		// 继续 install 落盘到 GateBox 证书目录(否则手动签过证书后会被误判失败)。
		if !force {
			i.mu.Lock()
			i.failCooldow[fqdn] = time.Now().Add(failCooldown)
			i.mu.Unlock()
		}
		return logFile, fmt.Errorf("acme.sh 签发失败: %w", issueErr)
	}
	// 安装证书到约定目录
	installArgs := i.homeArgs()
	installArgs = append(installArgs, "--install-cert", "-d", fqdn,
		"--cert-file", filepath.Join(p.Dir, "cert.pem"),
		"--key-file", p.Key,
		"--fullchain-file", p.Cert,
		"--reloadcmd", "true",
	)
	installOut, installErr := runCmd(ctx, i.BinPath, issueEnv, installArgs...)
	appendLog(installOut)
	if installErr != nil {
		return logFile, fmt.Errorf("acme.sh 安装证书失败: %w", installErr)
	}
	return logFile, nil
}

// writeIssueLog 为一次申请返回原始输出日志文件路径(<CertsDir>/logs/<时间戳>-<fqdn>.log)。
func (i *Issuer) writeIssueLog(fqdn string) (string, error) {
	ts := time.Now().Format("20060102-150405")
	p := filepath.Join(i.CertsDir, "logs", ts+"-"+fqdn+".log")
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return "", err
	}
	return p, nil
}

// run 执行命令并返回原始输出(CombinedOutput)。
func run(ctx context.Context, bin string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %v: %w (%s)", bin, args, err, trim(out))
	}
	return string(out), nil
}

func trim(b []byte) string {
	s := string(b)
	if len(s) > 300 {
		s = s[len(s)-300:]
	}
	return s
}

// providerEnv 返回 acme.sh 的 DNS provider 参数与该 provider 需要的环境变量。
func providerEnv(cred model.DNSCredential) (string, []string, error) {
	switch cred.Provider {
	case model.ProviderDNSPod:
		// 腾讯云 DNSPod:使用腾讯云 API 密钥(SecretId/SecretKey),走 acme.sh dns_tencent。
		// 旧式 DNSPod Token 凭据与此不兼容(会 401)——2026-09-10 确认本环境为腾讯云密钥。
		secretID := cred.Fields["id"]
		secretKey := cred.Fields["token"]
		if secretID == "" || secretKey == "" {
			return "", nil, errors.New("dnspod 凭证缺少腾讯云 SecretId/SecretKey(存于 id/token 字段)")
		}
		return "dns_tencent", []string{"Tencent_SecretId=" + secretID, "Tencent_SecretKey=" + secretKey}, nil
	case model.ProviderCloudflare:
		token := cred.Fields["token"]
		if token == "" {
			return "", nil, errors.New("cloudflare 凭证缺少 token")
		}
		return "dns_cf", []string{"CF_Token=" + token}, nil
	case model.ProviderAliyun:
		key := cred.Fields["accessKeyId"]
		secret := cred.Fields["accessKeySecret"]
		if key == "" || secret == "" {
			return "", nil, errors.New("aliyun 凭证缺少 accessKeyId/accessKeySecret")
		}
		return "dns_ali", []string{"ALI_KEY=" + key, "ALI_SECRET=" + secret}, nil
	default:
		return "", nil, fmt.Errorf("不支持的证书 DNS 供应商: %s", cred.Provider)
	}
}

// envMap 处理同名环境变量覆盖(key 已含值,无需额外逻辑)。
func envMap(env []string) []string {
	return env
}
