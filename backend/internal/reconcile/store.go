package reconcile

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/JiangBeta/gatebox/internal/repository"
)

// RunStore Run/Step 的持久化接口（L2 定义，L3 写入）。
type RunStore interface {
	Save(Run) error
	Get(id string) (Run, error)
	List(limit int) ([]Run, error)
}

// retention 保留策略：条数 + 天数双限（ADR-041 §7）。
const (
	runRetentionCount = 50
	runRetentionDays  = 7
)

// BoltRunStore 基于 BoltDB `runs` bucket 的实现。
//
// run ID 约定为 "<unixnano>-<短id>"，天然可排序（ListRunIDs 按 key 倒序）。
type BoltRunStore struct {
	repo *repository.Store
}

// NewBoltRunStore 构造 Run 存储。
func NewBoltRunStore(repo *repository.Store) *BoltRunStore {
	return &BoltRunStore{repo: repo}
}

// NewRunID 生成可排序的 run ID。
func NewRunID(now time.Time) string {
	return fmt.Sprintf("%020d-%s", now.UnixNano(), strings.ToLower(strconv.FormatInt(now.UnixNano()%0xFFFFFF, 36)))
}

// Save 写入并执行保留策略（Step.Events 落库裁剪为最近 20 条）。
func (s *BoltRunStore) Save(r Run) error {
	for i := range r.Steps {
		if len(r.Steps[i].Events) > 20 {
			r.Steps[i].Events = r.Steps[i].Events[len(r.Steps[i].Events)-20:]
		}
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := s.repo.SaveRun(r.ID, data); err != nil {
		return err
	}
	s.trim()
	return nil
}

// Get 读取一条 run。
func (s *BoltRunStore) Get(id string) (Run, error) {
	data, err := s.repo.GetRun(id)
	if err != nil {
		return Run{}, err
	}
	var r Run
	if err := json.Unmarshal(data, &r); err != nil {
		return Run{}, err
	}
	return r, nil
}

// List 返回最近的 run 列表（倒序）。
func (s *BoltRunStore) List(limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 20
	}
	ids, err := s.repo.ListRunIDs(limit)
	if err != nil {
		return nil, err
	}
	out := make([]Run, 0, len(ids))
	for _, id := range ids {
		if r, err := s.Get(id); err == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

// trim 删除超出保留策略的记录。
func (s *BoltRunStore) trim() {
	ids, err := s.repo.ListRunIDs(0)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -runRetentionDays).UnixNano()
	for i, id := range ids {
		tooMany := i >= runRetentionCount
		tooOld := nanoOf(id) < cutoff
		if tooMany || tooOld {
			_ = s.repo.DeleteRun(id)
		}
	}
}

func nanoOf(id string) int64 {
	part := id
	if idx := strings.IndexByte(id, '-'); idx > 0 {
		part = id[:idx]
	}
	n, _ := strconv.ParseInt(part, 10, 64)
	return n
}

// IsNotFound 判断错误是否为「未找到」。
func IsNotFound(err error) bool { return errors.Is(err, repository.ErrNotFound) }
