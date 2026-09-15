package repository

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/JiangBeta/gatebox/internal/model"
	bolt "go.etcd.io/bbolt"
)

// 变量统一迁移(ADR-035 §8):把历史 bucket `container_variables` 并入 `variables`。
// 幂等(meta 标记)、冲突可见(报告)、可回滚(旧 bucket 保留不删)。
const (
	metaKeyVariablesMigrated = "variables_migrated_adr035"
	metaKeyVariablesReport   = "variables_migration_report_adr035"
)

// MigrateVariables 执行一次性变量迁移。同名异值 / 键名不合法的存量项不自动导入,
// 记入迁移报告由用户在设置页处理;旧 bucket 原样保留作为只读备份。
func (s *Store) MigrateVariables() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		if meta == nil || meta.Get([]byte(metaKeyVariablesMigrated)) != nil {
			return nil // 已迁移(幂等)
		}
		report := model.VariableMigration{At: time.Now(), Conflicts: []model.VariableConflict{}}
		dst := tx.Bucket(bucketVariables)
		src := tx.Bucket(bucketContainerVariables)
		if src != nil && dst != nil {
			_ = src.ForEach(func(k, v []byte) error {
				var cv model.Variable
				if err := json.Unmarshal(v, &cv); err != nil {
					return nil // 跳过损坏记录
				}
				key := strings.TrimSpace(cv.Key)
				if key == "" {
					key = string(k)
				}
				if !model.ValidVariableKey(key) {
					report.Conflicts = append(report.Conflicts, model.VariableConflict{
						Key: key, Reason: "invalid-key",
						ContainerValue: cv.Value, Description: cv.Description,
					})
					return nil
				}
				if raw := dst.Get([]byte(key)); raw != nil {
					var gv model.Variable
					if err := json.Unmarshal(raw, &gv); err == nil && gv.Value == cv.Value {
						return nil // 同名同值:无需导入
					}
					gwVal := ""
					if err := json.Unmarshal(raw, &gv); err == nil {
						gwVal = gv.Value
					}
					report.Conflicts = append(report.Conflicts, model.VariableConflict{
						Key: key, Reason: "conflict",
						GatewayValue: gwVal, ContainerValue: cv.Value, Description: cv.Description,
					})
					return nil
				}
				cv.Key = key
				plain, err := json.Marshal(cv)
				if err != nil {
					return err
				}
				if err := dst.Put([]byte(key), plain); err != nil {
					return err
				}
				report.Migrated++
				return nil
			})
		}
		rep, err := json.Marshal(report)
		if err != nil {
			return err
		}
		if err := meta.Put([]byte(metaKeyVariablesReport), rep); err != nil {
			return err
		}
		return meta.Put([]byte(metaKeyVariablesMigrated), []byte("1"))
	})
}

// VariableMigrationReport 读取迁移报告;无报告(尚未迁移或已清除)返回 nil。
func (s *Store) VariableMigrationReport() (*model.VariableMigration, error) {
	var rep *model.VariableMigration
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMeta)
		if b == nil {
			return nil
		}
		raw := b.Get([]byte(metaKeyVariablesReport))
		if raw == nil {
			return nil
		}
		var r model.VariableMigration
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		if r.Conflicts == nil {
			r.Conflicts = []model.VariableConflict{}
		}
		rep = &r
		return nil
	})
	return rep, err
}

// ClearVariableMigrationReport 清除迁移报告(用户确认处理完毕)。
func (s *Store) ClearVariableMigrationReport() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMeta)
		if b == nil {
			return nil
		}
		return b.Delete([]byte(metaKeyVariablesReport))
	})
}
