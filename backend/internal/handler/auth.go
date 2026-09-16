package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 单管理员鉴权（ADR-030 §3 / architecture §11）：**可选启用**。
//
// 配置 admin_password_sha256（十六进制）后，/api/v1/* 需登录会话；
// 未配置时维持开放（内网开发形态，与既有行为一致）。
// 会话为无状态 HMAC 令牌，置 httpOnly Cookie；密钥由口令哈希派生，
// 改口令即失效全部会话。不使用 Casbin/RBAC。

const sessionCookie = "gatebox_session"

// Auth 鉴权器；nil 表示未启用。
type Auth struct {
	hashHex string
	key     []byte
	ttl     time.Duration
}

// NewAuth 构造鉴权器；passwordSha256Hex 为空或非法时返回 nil（不启用）。
func NewAuth(passwordSha256Hex string) *Auth {
	v := strings.TrimSpace(passwordSha256Hex)
	if v == "" {
		return nil
	}
	sum, err := hex.DecodeString(v)
	if err != nil || len(sum) != sha256.Size {
		return nil
	}
	key := sha256.Sum256(append([]byte("gatebox-session:"), sum...))
	return &Auth{hashHex: hex.EncodeToString(sum), key: key[:], ttl: 7 * 24 * time.Hour}
}

// Enabled 是否启用鉴权。
func (a *Auth) Enabled() bool { return a != nil }

// Register 注册鉴权路由（始终注册，便于前端探测启用状态）。
func (a *Auth) Register(mux *http.ServeMux) {
	if a == nil {
		mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "authed": true})
		})
		return
	}
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("POST /api/v1/auth/logout", a.logout)
	mux.HandleFunc("GET /api/v1/auth/me", a.me)
}

// Middleware 保护 /api/v1/*；放行登录、健康检查与插件投影（后者用 plugin token 自鉴权）。
func (a *Auth) Middleware(next http.Handler) http.Handler {
	if a == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if !strings.HasPrefix(p, "/api/v1/") || a.exempt(p) {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil || !a.valid(c.Value) {
			writeErrCode(w, http.StatusUnauthorized, "UNAUTHORIZED", "未登录或会话已过期")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// exempt 放行路径：登录/状态、健康检查、插件投影（plugin token 鉴权）。
func (a *Auth) exempt(p string) bool {
	switch {
	case p == "/api/v1/auth/login", p == "/api/v1/auth/logout", p == "/api/v1/auth/me":
		return true
	case p == "/api/v1/health":
		return true
	case strings.HasPrefix(p, "/api/v1/extensions/me/projection/"):
		return true
	default:
		return false
	}
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErrCode(w, http.StatusBadRequest, "BAD_REQUEST", "请求体解析失败")
		return
	}
	sum := sha256.Sum256([]byte(in.Password))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(a.hashHex)) != 1 {
		writeErrCode(w, http.StatusUnauthorized, "BAD_CREDENTIALS", "口令错误")
		return
	}
	tok := a.token(time.Now().Add(a.ttl).Unix())
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int(a.ttl.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Auth) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Auth) me(w http.ResponseWriter, r *http.Request) {
	authed := false
	if c, err := r.Cookie(sessionCookie); err == nil && a.valid(c.Value) {
		authed = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "authed": authed})
}

// token 生成 `exp.mac` 形式的无状态会话令牌。
func (a *Auth) token(exp int64) string {
	mac := hmac.New(sha256.New, a.key)
	fmt.Fprintf(mac, "%d", exp)
	return strconv.FormatInt(exp, 10) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// valid 校验令牌的过期时间与 MAC。
func (a *Auth) valid(tok string) bool {
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		return false
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	mac := hmac.New(sha256.New, a.key)
	fmt.Fprintf(mac, "%d", exp)
	want, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	return hmac.Equal(want, mac.Sum(nil))
}
