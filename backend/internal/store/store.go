// Package store 提供 BoltDB 存储与凭证加密。
package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/JiangBeta/gatebox/internal/models"
	bolt "go.etcd.io/bbolt"
)

var (
	bucketDomains     = []byte("domains")
	bucketCredentials = []byte("dns_credentials")
	bucketRegistries  = []byte("registries")
	bucketCompose     = []byte("compose_instances")
	bucketApps        = []byte("apps")
	bucketServices    = []byte("services")
	bucketFragments   = []byte("fragments")
	bucketVariables   = []byte("variables") // 网关变量(网关页)
	bucketFragToggles = []byte("fragment_toggles")

	// bucketContainerVariables 容器页变量,与网关 bucketVariables 完全独立(网关/容器各自维护)。
	bucketContainerVariables = []byte("container_variables")
	bucketPortBindings       = []byte("port_bindings")
	bucketCertLogs           = []byte("cert_logs")
)

// ErrNotFound 记录不存在。
var ErrNotFound = errors.New("not found")

// Store BoltDB 存储 + 凭证 AES-GCM 加密。
type Store struct {
	db   *bolt.DB
	aead cipher.AEAD
}

// Open 打开/创建数据库并加载加密密钥。
func Open(dataDir string) (*Store, error) {
	dbPath := filepath.Join(dataDir, "db", "gatebox.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := bolt.Open(dbPath, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketDomains, bucketCredentials, bucketRegistries, bucketCompose, bucketApps, bucketServices, bucketFragments, bucketVariables, bucketFragToggles, bucketContainerVariables} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		db.Close()
		return nil, err
	}

	key, err := loadOrCreateKey(dataDir)
	if err != nil {
		db.Close()
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		db.Close()
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, aead: aead}, nil
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

// loadOrCreateKey 读取或生成 32 字节 AES 密钥,持久化到 DATA_DIR/db/secret.key。
func loadOrCreateKey(dataDir string) ([]byte, error) {
	keyPath := filepath.Join(dataDir, "db", "secret.key")
	if raw, err := os.ReadFile(keyPath); err == nil {
		key := make([]byte, 32)
		if _, err := hex.Decode(key, raw); err != nil {
			return nil, fmt.Errorf("解析密钥失败: %w", err)
		}
		return key, nil
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(key)), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// newID 生成随机十六进制 ID。
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *Store) decrypt(ciphertext []byte) ([]byte, error) {
	ns := s.aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("密文太短")
	}
	nonce, body := ciphertext[:ns], ciphertext[ns:]
	return s.aead.Open(nil, nonce, body, nil)
}

// --- 凭证 ---

// SaveCredential 创建或更新凭证(整体加密存储)。
func (s *Store) SaveCredential(c *models.DNSCredential) error {
	plain, err := json.Marshal(c)
	if err != nil {
		return err
	}
	enc, err := s.encrypt(plain)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketCredentials).Put([]byte(c.ID), enc)
	})
}

// ListCredentials 列出所有凭证。
func (s *Store) ListCredentials() ([]models.DNSCredential, error) {
	var out []models.DNSCredential
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketCredentials).ForEach(func(_, v []byte) error {
			c, err := s.decodeCredential(v)
			if err != nil {
				return err
			}
			out = append(out, *c)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []models.DNSCredential{}
	}
	return out, nil
}

// GetCredential 获取单个凭证。
func (s *Store) GetCredential(id string) (*models.DNSCredential, error) {
	var c *models.DNSCredential
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketCredentials).Get([]byte(id))
		if v == nil {
			return ErrNotFound
		}
		dec, err := s.decodeCredential(v)
		if err != nil {
			return err
		}
		c = dec
		return nil
	})
	return c, err
}

// DeleteCredential 删除凭证。
func (s *Store) DeleteCredential(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketCredentials).Delete([]byte(id))
	})
}

func (s *Store) decodeCredential(v []byte) (*models.DNSCredential, error) {
	plain, err := s.decrypt(v)
	if err != nil {
		return nil, err
	}
	var c models.DNSCredential
	if err := json.Unmarshal(plain, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// --- 私有镜像仓库(docs §3.3.1) ---

// SaveRegistry 创建或更新仓库凭证(整体加密存储)。
func (s *Store) SaveRegistry(r *models.Registry) error {
	plain, err := json.Marshal(r)
	if err != nil {
		return err
	}
	enc, err := s.encrypt(plain)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRegistries).Put([]byte(r.ID), enc)
	})
}

// ListRegistries 列出所有仓库凭证。
func (s *Store) ListRegistries() ([]models.Registry, error) {
	var out []models.Registry
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRegistries).ForEach(func(_, v []byte) error {
			r, err := s.decodeRegistry(v)
			if err != nil {
				return err
			}
			out = append(out, *r)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []models.Registry{}
	}
	return out, nil
}

// GetRegistry 获取单个仓库凭证。
func (s *Store) GetRegistry(id string) (*models.Registry, error) {
	var r *models.Registry
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketRegistries).Get([]byte(id))
		if v == nil {
			return ErrNotFound
		}
		dec, err := s.decodeRegistry(v)
		if err != nil {
			return err
		}
		r = dec
		return nil
	})
	return r, err
}

// DeleteRegistry 删除仓库凭证。
func (s *Store) DeleteRegistry(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRegistries).Delete([]byte(id))
	})
}

func (s *Store) decodeRegistry(v []byte) (*models.Registry, error) {
	plain, err := s.decrypt(v)
	if err != nil {
		return nil, err
	}
	var r models.Registry
	if err := json.Unmarshal(plain, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// --- 域名 ---

// SaveDomain 创建或更新域名。
func (s *Store) SaveDomain(d *models.Domain) error {
	plain, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketDomains).Put([]byte(d.ID), plain)
	})
}

// ListDomains 列出所有域名。
func (s *Store) ListDomains() ([]models.Domain, error) {
	var out []models.Domain
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketDomains).ForEach(func(_, v []byte) error {
			var d models.Domain
			if err := json.Unmarshal(v, &d); err != nil {
				return err
			}
			out = append(out, d)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []models.Domain{}
	}
	return out, nil
}

// GetDomain 获取单个域名。
func (s *Store) GetDomain(id string) (*models.Domain, error) {
	var d *models.Domain
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketDomains).Get([]byte(id))
		if v == nil {
			return ErrNotFound
		}
		var dec models.Domain
		if err := json.Unmarshal(v, &dec); err != nil {
			return err
		}
		d = &dec
		return nil
	})
	return d, err
}

// DeleteDomain 删除域名。
func (s *Store) DeleteDomain(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketDomains).Delete([]byte(id))
	})
}

// --- 编排项目(docs §2.2) ---

// SaveComposeInstance 创建或更新编排项目(明文存储,无敏感信息)。
func (s *Store) SaveComposeInstance(c *models.ComposeInstance) error {
	plain, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketCompose).Put([]byte(c.ProjectName), plain)
	})
}

// ListComposeInstances 列出所有编排项目。
func (s *Store) ListComposeInstances() ([]models.ComposeInstance, error) {
	var out []models.ComposeInstance
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketCompose).ForEach(func(_, v []byte) error {
			var c models.ComposeInstance
			if err := json.Unmarshal(v, &c); err != nil {
				return err
			}
			out = append(out, c)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []models.ComposeInstance{}
	}
	return out, nil
}

// GetComposeInstance 获取单个编排项目。
func (s *Store) GetComposeInstance(project string) (*models.ComposeInstance, error) {
	var c *models.ComposeInstance
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketCompose).Get([]byte(project))
		if v == nil {
			return ErrNotFound
		}
		var dec models.ComposeInstance
		if err := json.Unmarshal(v, &dec); err != nil {
			return err
		}
		c = &dec
		return nil
	})
	return c, err
}

// DeleteComposeInstance 删除编排项目记录(仅删 DB 记录,不碰 docker 或文件)。
func (s *Store) DeleteComposeInstance(project string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketCompose).Delete([]byte(project))
	})
}

// --- 网关单位(docs/gateway.md §2):App / Service / Fragment / Variable ---
// 实现见 gateway.go。
