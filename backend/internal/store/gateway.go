package store

import (
	"encoding/json"
	"errors"
	"sort"

	bolt "go.etcd.io/bbolt"

	"github.com/JiangBeta/gatebox/internal/models"
)

// --- App(应用分组) ---

// SaveApp 创建或更新应用。
func (s *Store) SaveApp(a *models.App) error {
	plain, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketApps).Put([]byte(a.ID), plain)
	})
}

// ListApps 列出所有应用(按名称排序,保证 UI 稳定)。
func (s *Store) ListApps() ([]models.App, error) {
	var out []models.App
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketApps).ForEach(func(_, v []byte) error {
			var a models.App
			if err := json.Unmarshal(v, &a); err != nil {
				return err
			}
			out = append(out, a)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []models.App{}
	}
	return out, nil
}

// GetApp 获取单个应用。
func (s *Store) GetApp(id string) (*models.App, error) {
	var a *models.App
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketApps).Get([]byte(id))
		if v == nil {
			return ErrNotFound
		}
		var dec models.App
		if err := json.Unmarshal(v, &dec); err != nil {
			return err
		}
		a = &dec
		return nil
	})
	return a, err
}

// DeleteApp 删除应用(不校验子服务,由调用方做级联)。
func (s *Store) DeleteApp(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketApps).Delete([]byte(id))
	})
}

// --- Service(服务) ---

// SaveService 创建或更新服务。
func (s *Store) SaveService(r *models.Service) error {
	plain, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketServices).Put([]byte(r.ID), plain)
	})
}

// ListServices 列出所有服务。
func (s *Store) ListServices() ([]models.Service, error) {
	var out []models.Service
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketServices).ForEach(func(_, v []byte) error {
			var r models.Service
			if err := json.Unmarshal(v, &r); err != nil {
				return err
			}
			out = append(out, r)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []models.Service{}
	}
	return out, nil
}

// ListServicesByApp 列出某应用下的所有服务。
func (s *Store) ListServicesByApp(appID string) ([]models.Service, error) {
	all, err := s.ListServices()
	if err != nil {
		return nil, err
	}
	var out []models.Service
	for _, r := range all {
		if r.AppID == appID {
			out = append(out, r)
		}
	}
	if out == nil {
		out = []models.Service{}
	}
	return out, nil
}

// GetService 获取单个服务。
func (s *Store) GetService(id string) (*models.Service, error) {
	var r *models.Service
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketServices).Get([]byte(id))
		if v == nil {
			return ErrNotFound
		}
		var dec models.Service
		if err := json.Unmarshal(v, &dec); err != nil {
			return err
		}
		r = &dec
		return nil
	})
	return r, err
}

// DeleteService 删除服务。
func (s *Store) DeleteService(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketServices).Delete([]byte(id))
	})
}

// --- Fragment(Caddy 片段) ---

// SaveFragment 创建或更新 Caddy 片段(整体加密存储,code 可能含密钥)。
func (s *Store) SaveFragment(f *models.Fragment) error {
	plain, err := json.Marshal(f)
	if err != nil {
		return err
	}
	enc, err := s.encrypt(plain)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketFragments).Put([]byte(f.ID), enc)
	})
}

// ListFragments 列出所有用户片段。
func (s *Store) ListFragments() ([]models.Fragment, error) {
	var out []models.Fragment
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketFragments).ForEach(func(_, v []byte) error {
			plain, err := s.decrypt(v)
			if err != nil {
				return err
			}
			var f models.Fragment
			if err := json.Unmarshal(plain, &f); err != nil {
				return err
			}
			out = append(out, f)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []models.Fragment{}
	}
	return out, nil
}

// GetFragment 获取单个用户片段。
func (s *Store) GetFragment(id string) (*models.Fragment, error) {
	var f *models.Fragment
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketFragments).Get([]byte(id))
		if v == nil {
			return ErrNotFound
		}
		plain, err := s.decrypt(v)
		if err != nil {
			return err
		}
		var dec models.Fragment
		if err := json.Unmarshal(plain, &dec); err != nil {
			return err
		}
		f = &dec
		return nil
	})
	return f, err
}

// DeleteFragment 删除用户片段。
func (s *Store) DeleteFragment(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketFragments).Delete([]byte(id))
	})
}

// SaveFragmentToggle 保存内置片段的默认开关覆盖(明文,不含敏感信息)。
func (s *Store) SaveFragmentToggle(id string, t models.BuiltinFragmentToggle) error {
	plain, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketFragToggles).Put([]byte(id), plain)
	})
}

// ListFragmentToggles 列出所有内置片段默认开关覆盖。
func (s *Store) ListFragmentToggles() (map[string]models.BuiltinFragmentToggle, error) {
	out := map[string]models.BuiltinFragmentToggle{}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketFragToggles).ForEach(func(k, v []byte) error {
			var t models.BuiltinFragmentToggle
			if err := json.Unmarshal(v, &t); err != nil {
				return err
			}
			out[string(k)] = t
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetFragmentToggle 获取单个内置片段默认开关覆盖。
func (s *Store) GetFragmentToggle(id string) (models.BuiltinFragmentToggle, bool, error) {
	var t models.BuiltinFragmentToggle
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketFragToggles).Get([]byte(id))
		if v == nil {
			return ErrNotFound
		}
		return json.Unmarshal(v, &t)
	})
	if errors.Is(err, ErrNotFound) {
		return t, false, nil
	}
	if err != nil {
		return t, false, err
	}
	return t, true, nil
}

// --- Variable(网关页用户变量) ---
// 与容器页变量(bucketContainerVariables)各用独立 bucket,互不影响。

// SaveVariable 创建或更新网关用户变量(key 为主键)。
func (s *Store) SaveVariable(v *models.Variable) error {
	return s.saveVariableIn(bucketVariables, v)
}

// ListVariables 列出所有网关用户变量(按 key 排序)。
func (s *Store) ListVariables() ([]models.Variable, error) {
	return s.listVariablesIn(bucketVariables)
}

// GetVariable 获取单个网关用户变量。
func (s *Store) GetVariable(key string) (*models.Variable, error) {
	return s.getVariableIn(bucketVariables, key)
}

// DeleteVariable 删除网关用户变量。
func (s *Store) DeleteVariable(key string) error {
	return s.deleteVariableIn(bucketVariables, key)
}

// --- ContainerVariable(容器页用户变量,独立 bucket) ---

// SaveContainerVariable 创建或更新容器用户变量(key 为主键)。
func (s *Store) SaveContainerVariable(v *models.Variable) error {
	return s.saveVariableIn(bucketContainerVariables, v)
}

// ListContainerVariables 列出所有容器用户变量(按 key 排序)。
func (s *Store) ListContainerVariables() ([]models.Variable, error) {
	return s.listVariablesIn(bucketContainerVariables)
}

// GetContainerVariable 获取单个容器用户变量。
func (s *Store) GetContainerVariable(key string) (*models.Variable, error) {
	return s.getVariableIn(bucketContainerVariables, key)
}

// DeleteContainerVariable 删除容器用户变量。
func (s *Store) DeleteContainerVariable(key string) error {
	return s.deleteVariableIn(bucketContainerVariables, key)
}

// --- PortBinding(网关 → 端口,ADR-026) ---

// SavePortBinding 创建或更新协议端口记录(protocol 为主键)。
func (s *Store) SavePortBinding(p *models.PortBinding) error {
	if p == nil {
		return nil
	}
	plain, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketPortBindings)
		if err != nil {
			return err
		}
		return b.Put([]byte(p.Protocol), plain)
	})
}

// ListPortBindings 列出全部协议端口记录(按协议名排序)。
func (s *Store) ListPortBindings() ([]models.PortBinding, error) {
	var out []models.PortBinding
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketPortBindings)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var p models.PortBinding
			if err := json.Unmarshal(v, &p); err != nil {
				return nil // 跳过损坏记录
			}
			out = append(out, p)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Protocol < out[j].Protocol })
	return out, err
}

// DeletePortBinding 删除一条协议端口记录。
func (s *Store) DeletePortBinding(protocol string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketPortBindings)
		if err != nil {
			return err
		}
		return b.Delete([]byte(protocol))
	})
}

// --- CertLog(SSL 证书操作日志) ---

// SaveCertLog 追加一条证书操作日志。
func (s *Store) SaveCertLog(l *models.CertLog) error {
	if l == nil {
		return nil
	}
	plain, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketCertLogs)
		if err != nil {
			return err
		}
		return b.Put([]byte(l.ID), plain)
	})
}

// ListCertLogs 列出最近 limit 条证书日志(按时间倒序)。
func (s *Store) ListCertLogs(limit int) ([]models.CertLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var out []models.CertLog
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketCertLogs)
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var l models.CertLog
			if err := json.Unmarshal(v, &l); err == nil {
				out = append(out, l)
				if len(out) >= limit {
					break
				}
			}
		}
		return nil
	})
	if out == nil {
		out = []models.CertLog{}
	}
	return out, err
}

// GetCertLog 按 ID 读取一条证书日志。
func (s *Store) GetCertLog(id string) (*models.CertLog, error) {
	var row *models.CertLog
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketCertLogs)
		if b == nil {
			return ErrNotFound
		}
		raw := b.Get([]byte(id))
		if raw == nil {
			return ErrNotFound
		}
		var dec models.CertLog
		if err := json.Unmarshal(raw, &dec); err != nil {
			return err
		}
		row = &dec
		return nil
	})
	return row, err
}

// --- 底层共享实现 ---

func (s *Store) saveVariableIn(bkt []byte, v *models.Variable) error {
	plain, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bkt).Put([]byte(v.Key), plain)
	})
}

func (s *Store) listVariablesIn(bkt []byte) ([]models.Variable, error) {
	var out []models.Variable
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bkt).ForEach(func(_, v []byte) error {
			var var_ models.Variable
			if err := json.Unmarshal(v, &var_); err != nil {
				return err
			}
			out = append(out, var_)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []models.Variable{}
	}
	return out, nil
}

func (s *Store) getVariableIn(bkt []byte, key string) (*models.Variable, error) {
	var v *models.Variable
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bkt).Get([]byte(key))
		if raw == nil {
			return ErrNotFound
		}
		var dec models.Variable
		if err := json.Unmarshal(raw, &dec); err != nil {
			return err
		}
		v = &dec
		return nil
	})
	return v, err
}

func (s *Store) deleteVariableIn(bkt []byte, key string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bkt).Delete([]byte(key))
	})
}
