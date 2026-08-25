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
		for _, b := range [][]byte{bucketDomains, bucketCredentials} {
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
