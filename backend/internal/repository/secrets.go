package repository

import (
	"bytes"
	"errors"

	bolt "go.etcd.io/bbolt"
)

// V4.1 只写字段的密文存储（ADR-043 §3「密码为只写字段：不回显、不进对象文本」）。
//
// 值经 store 的 AES-GCM 加密，键为 "<kind> NUL <id>"。当前仅 user 密码哈希使用。
// 与 bucketObjects 物理分开：对象文本会经 API 回前端，密码绝不能落在那里。

// PutSecret 覆盖写入一条密文秘密。
func (s *Store) PutSecret(kind, id string, value []byte) error {
	ct, err := s.encrypt(value)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketSecrets)
		if err != nil {
			return err
		}
		return b.Put(objKey(kind, id), ct)
	})
}

// GetSecret 读取一条密文秘密；不存在返回 ErrNotFound。
func (s *Store) GetSecret(kind, id string) ([]byte, error) {
	var out []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSecrets)
		if b == nil {
			return ErrNotFound
		}
		ct := b.Get(objKey(kind, id))
		if ct == nil {
			return ErrNotFound
		}
		plain, err := s.decrypt(ct)
		if err != nil {
			return err
		}
		out = append([]byte(nil), plain...)
		return nil
	})
	return out, err
}

// DeleteSecret 删除一条密文秘密；不存在不算错（幂等）。
func (s *Store) DeleteSecret(kind, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSecrets)
		if b == nil {
			return nil
		}
		return b.Delete(objKey(kind, id))
	})
}

// ListSecretIDs 返回某 kind 的全部秘密 id（按字典序）。
func (s *Store) ListSecretIDs(kind string) ([]string, error) {
	var out []string
	prefix := []byte(kind + "\x00")
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSecrets)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, _ []byte) error {
			if !bytes.HasPrefix(k, prefix) {
				return nil
			}
			out = append(out, string(k[len(prefix):]))
			return nil
		})
	})
	return out, err
}

// HasSecret 判断是否存在（不返回值，避免把秘密读进内存做无谓比较）。
func (s *Store) HasSecret(kind, id string) (bool, error) {
	var ok bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSecrets)
		if b == nil {
			return nil
		}
		ok = b.Get(objKey(kind, id)) != nil
		return nil
	})
	return ok, err
}

// SecretStore 只写秘密的窄接口（便于 objects.Service 依赖倒置与测试替身）。
type SecretStore interface {
	PutSecret(kind, id string, value []byte) error
	GetSecret(kind, id string) ([]byte, error)
	DeleteSecret(kind, id string) error
	HasSecret(kind, id string) (bool, error)
}

// ErrSecretNotFound 秘密不存在（包一层以便 errors.Is 判断，与对象层语义区分）。
var ErrSecretNotFound = errors.New("secret not found")
