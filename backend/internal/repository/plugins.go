package repository

import (
	"encoding/json"

	"github.com/JiangBeta/gatebox/internal/model"
	bolt "go.etcd.io/bbolt"
)

// bucketPlugins 插件状态。
var bucketPlugins = []byte("plugins")

// SavePluginState 写入插件状态。
func (s *Store) SavePluginState(p *model.PluginState) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketPlugins)
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		return b.Put([]byte(p.ID), raw)
	})
}

// GetPluginState 读取插件状态；不存在返回 ErrNotFound。
func (s *Store) GetPluginState(id string) (*model.PluginState, error) {
	var out *model.PluginState
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketPlugins).Get([]byte(id))
		if raw == nil {
			return ErrNotFound
		}
		var p model.PluginState
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		out = &p
		return nil
	})
	return out, err
}

// ListPluginStates 列出全部插件状态。
func (s *Store) ListPluginStates() ([]model.PluginState, error) {
	out := make([]model.PluginState, 0)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketPlugins).ForEach(func(_, raw []byte) error {
			var p model.PluginState
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil
			}
			out = append(out, p)
			return nil
		})
	})
	return out, err
}

// DeletePluginState 删除插件状态。
func (s *Store) DeletePluginState(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketPlugins).Delete([]byte(id))
	})
}
