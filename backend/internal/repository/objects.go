package repository

import (
	"bytes"
	"encoding/json"
	"sort"

	bolt "go.etcd.io/bbolt"
)

// V4.1 通用对象层（ADR-043 §2）。所有 kind 落同一个 bucket，键为 "<kind>\x00<id>"，
// 避免每加一个对象类型就动一次 schema；值是 repository 层不解析的 JSON。
//
// 事务边界由 Service 负责（校验 + 引用级联必须原子），本层只提供单事务内的批操作。

// Obj 仓储层不透明对象载荷（Service 层解码为 objects.Object）。
type Obj struct {
	Kind string
	ID   string
	Body []byte
}

func objKey(kind, id string) []byte {
	return []byte(kind + "\x00" + id)
}

// SaveObjects 在单个事务里覆盖写入一批对象。
func (s *Store) SaveObjects(list []Obj) error {
	if len(list) == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketObjects)
		for _, o := range list {
			if err := b.Put(objKey(o.Kind, o.ID), o.Body); err != nil {
				return err
			}
		}
		return nil
	})
}

// SaveObject 覆盖写入单个对象。
func (s *Store) SaveObject(kind, id string, body []byte) error {
	return s.SaveObjects([]Obj{{Kind: kind, ID: id, Body: body}})
}

// GetObject 读取单个对象。
func (s *Store) GetObject(kind, id string) ([]byte, error) {
	var out []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketObjects).Get(objKey(kind, id))
		if v == nil {
			return ErrNotFound
		}
		out = bytes.Clone(v)
		return nil
	})
	return out, err
}

// ListObjects 返回某 kind 的全部对象载荷（按 id 升序）。
func (s *Store) ListObjects(kind string) ([]Obj, error) {
	prefix := []byte(kind + "\x00")
	var out []Obj
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketObjects).Cursor()
		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			out = append(out, Obj{Kind: kind, ID: string(k[len(prefix):]), Body: bytes.Clone(v)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AllObjects 返回全部 kind 的对象载荷（供引用索引全量重建）。
func (s *Store) AllObjects() ([]Obj, error) {
	var out []Obj
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketObjects).ForEach(func(k, v []byte) error {
			i := bytes.IndexByte(k, 0)
			if i < 0 {
				return nil
			}
			out = append(out, Obj{Kind: string(k[:i]), ID: string(k[i+1:]), Body: bytes.Clone(v)})
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// DeleteObject 删除单个对象。
func (s *Store) DeleteObject(kind, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketObjects).Delete(objKey(kind, id))
	})
}

// UpdateObjects 原子批改：在同一个写事务里应用 fn 的结果。
//
// fn 收到当前全部对象（只读语义），返回要写入的对象与要删除的键。
// 这样「删除被引用对象时同步清理引用」这类跨对象改写才能原子生效。
func (s *Store) UpdateObjects(fn func(cur []Obj) (puts []Obj, dels []Obj, err error)) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketObjects)
		var cur []Obj
		if err := b.ForEach(func(k, v []byte) error {
			i := bytes.IndexByte(k, 0)
			if i < 0 {
				return nil
			}
			cur = append(cur, Obj{Kind: string(k[:i]), ID: string(k[i+1:]), Body: bytes.Clone(v)})
			return nil
		}); err != nil {
			return err
		}
		sort.SliceStable(cur, func(i, j int) bool {
			if cur[i].Kind != cur[j].Kind {
				return cur[i].Kind < cur[j].Kind
			}
			return cur[i].ID < cur[j].ID
		})
		puts, dels, err := fn(cur)
		if err != nil {
			return err
		}
		for _, o := range puts {
			if o.Body == nil {
				b, err := json.Marshal(o)
				if err != nil {
					return err
				}
				o.Body = b
			}
			if err := b.Put(objKey(o.Kind, o.ID), o.Body); err != nil {
				return err
			}
		}
		for _, d := range dels {
			if err := b.Delete(objKey(d.Kind, d.ID)); err != nil {
				return err
			}
		}
		return nil
	})
}
