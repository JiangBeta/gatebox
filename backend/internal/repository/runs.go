package repository

import (
	"bytes"
	"sort"

	bolt "go.etcd.io/bbolt"
)

// Run 记录（v4 调和）：键为 runID，值为 JSON 字节。仓储层不解析内容。

// SaveRun 写入/覆盖一条 run 记录。
func (s *Store) SaveRun(id string, data []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRuns).Put([]byte(id), data)
	})
}

// GetRun 读取一条 run 记录（JSON）。
func (s *Store) GetRun(id string) ([]byte, error) {
	var out []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketRuns).Get([]byte(id))
		if v == nil {
			return ErrNotFound
		}
		out = bytes.Clone(v)
		return nil
	})
	return out, err
}

// DeleteRun 删除一条 run 记录。
func (s *Store) DeleteRun(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRuns).Delete([]byte(id))
	})
}

// ListRunIDs 返回起始时间倒序的 run ID 列表（key 约定为 "<unixnano>:<id>"）。
func (s *Store) ListRunIDs(limit int) ([]string, error) {
	ids := make([]string, 0)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRuns).ForEach(func(k, _ []byte) error {
			ids = append(ids, string(k))
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	if limit > 0 && len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}
