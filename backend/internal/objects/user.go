package objects

import (
	"errors"
	"fmt"
	"strings"

	"github.com/JiangBeta/gatebox/internal/repository"
	"github.com/JiangBeta/gatebox/internal/typespec"
	"golang.org/x/crypto/bcrypt"
)

// 用户密码：写-only 字段（model/user.yaml `writeOnly.password`）。
//
// 明文永不落库：保存时算 bcrypt(cost 14) 存进 secrets bucket，密文再经 store 的
// AES-GCM 加密一层。对象文本（spec/status）里只留 passwordSet / hashAlgorithm 标记，
// 因此 GET /api/v1/objects/user/x 不可能泄露密码。
//
// 前端语义（ADR-043 §3）：
//   - 新建：不传 password → 拒绝（schema required）。
//   - 编辑：不传或传空串 → 不改密码（model/user.yaml「编辑时留空表示不修改」）。

// PasswordPolicy 密码强度要求（与 model/user.yaml 的 minLength/confirm 对齐）。
const (
	PasswordMinLength = 8
	bcryptCost        = 14
)

// ErrPasswordRequired 新建用户未提供密码。
var ErrPasswordRequired = errors.New("objects: 新建用户必须提供密码")

// ErrPasswordWeak 密码不满足最小长度。
var ErrPasswordWeak = fmt.Errorf("objects: 密码至少 %d 位", PasswordMinLength)

// SecretStore 只写秘密的窄接口。
type SecretStore interface {
	PutSecret(kind, id string, value []byte) error
	GetSecret(kind, id string) ([]byte, error)
	DeleteSecret(kind, id string) error
	HasSecret(kind, id string) (bool, error)
}

// WithSecrets 注入只写秘密存储。
func (s *Service) WithSecrets(sec SecretStore) *Service {
	s.secrets = sec
	return s
}

// CreateUser 建用户并设置初始密码。
func (s *Service) CreateUser(spec map[string]any, password string) (Object, error) {
	if strings.TrimSpace(password) == "" {
		return Object{}, ErrPasswordRequired
	}
	if err := checkPassword(password); err != nil {
		return Object{}, err
	}
	o, err := s.Create(typespec.KindUser, spec)
	if err != nil {
		return Object{}, err
	}
	if err := s.setPassword(o.ID, password); err != nil {
		// 密码写失败不留半个用户（否则会得到一个永远登录不了的账户）。
		_, _ = s.Delete(typespec.KindUser, o.ID, true)
		return Object{}, err
	}
	return s.Get(typespec.KindUser, o.ID)
}

// SetPassword 重设用户密码。
func (s *Service) SetPassword(id, password string) error {
	if err := checkPassword(password); err != nil {
		return err
	}
	return s.setPassword(id, password)
}

// PasswordHash 供渲染器解析 basic_auth 账户（内部链路，API 不暴露）。
func (s *Service) PasswordHash(id string) (string, error) {
	if s.secrets == nil {
		return "", fmt.Errorf("未配置秘密存储")
	}
	b, err := s.secrets.GetSecret(typespec.KindUser, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", fmt.Errorf("用户 %q 未设置密码", id)
		}
		return "", err
	}
	return string(b), nil
}

func (s *Service) setPassword(id, password string) error {
	if s.secrets == nil {
		return fmt.Errorf("未配置秘密存储")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return fmt.Errorf("生成密码哈希失败: %w", err)
	}
	if err := s.secrets.PutSecret(typespec.KindUser, id, hash); err != nil {
		return err
	}
	// 只回写「是否已设置」标记，哈希本身不进对象文本。
	return s.SetStatus(typespec.KindUser, id, map[string]any{
		"passwordSet":   true,
		"hashAlgorithm": "bcrypt",
	})
}

// CheckPassword 暴露密码强度校验给 handler 层（写前先拒，避免半落库）。
func CheckPassword(p string) error { return checkPassword(p) }

func checkPassword(p string) error {
	if len([]rune(p)) < PasswordMinLength {
		return ErrPasswordWeak
	}
	return nil
}
