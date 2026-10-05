package migration

import (
	"strings"

	"github.com/JiangBeta/gatebox/internal/objects"
	"github.com/JiangBeta/gatebox/internal/typespec"
)

// ApplyResult 执行结果。
type ApplyResult struct {
	Created   []string  `json:"created"`
	Skipped   []string  `json:"skipped"`
	Overwrote []string  `json:"overwritten"`
	Failed    []Failure `json:"failed"`
}

// Failure 一条写失败（多半是 schema 校验不过）。
type Failure struct {
	Target string `json:"target"`
	Reason string `json:"reason"`
}

// Normalize 把空切片补成空数组：JSON 里给 [] 而不是 null，
// 省得消费方到处判 null（CLI 的 --json 输出直接吃这个结构）。
func (r *ApplyResult) Normalize() {
	if r.Created == nil {
		r.Created = []string{}
	}
	if r.Skipped == nil {
		r.Skipped = []string{}
	}
	if r.Overwrote == nil {
		r.Overwrote = []string{}
	}
	if r.Failed == nil {
		r.Failed = []Failure{}
	}
}

// Applied 实际写入条数。
func (r ApplyResult) Applied() int { return len(r.Created) + len(r.Overwrote) }

// OK 是否全部成功。
func (r ApplyResult) OK() bool { return len(r.Failed) == 0 }

// Apply 按计划写对象。
//
// 走 svc.Put（无唯一性检查的直写）而不是 svc.Create：迁移要的是「按计划幂等写入」，
// 而不是业务层的「重名即拒」。但**仍跑 schema 校验**——悬空引用/必填缺失写进去
// 也是坏数据，不如当场报告。
//
// 引用存在性按「库里已有 ∪ 本次计划里有」判定：计划内部引用（route→service、
// route→entrypoint、route→domain）本来就靠这个通过，同时又不会像"一律放行"
// 那样把 credentialId 这种谁也满足不了的引用悄悄写进去。
func Apply(svc *objects.Service, plan Plan, force bool) (ApplyResult, error) {
	planned := make(map[string]bool, len(plan.Planned))
	for _, p := range plan.Planned {
		planned[p.Kind+"/"+p.ID] = true
	}
	exists := func(kind, id string) bool {
		if planned[kind+"/"+id] {
			return true
		}
		_, err := svc.Get(kind, id)
		return err == nil
	}

	var res ApplyResult
	for _, p := range plan.Planned {
		target := p.Kind + "/" + p.ID
		if p.Action == ActionSkip && !force {
			res.Skipped = append(res.Skipped, target)
			continue
		}
		if errs := svc.Spec().Validate(p.Kind, p.Object.Spec, exists); len(errs) > 0 {
			res.Failed = append(res.Failed, Failure{Target: target, Reason: flatten(errs)})
			continue
		}
		if err := svc.Put(p.Object); err != nil {
			res.Failed = append(res.Failed, Failure{Target: target, Reason: err.Error()})
			continue
		}
		if p.Action == ActionSkip {
			res.Overwrote = append(res.Overwrote, target)
		} else {
			res.Created = append(res.Created, target)
		}
	}
	return res, nil
}

func flatten(errs []typespec.FieldError) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, e.Field+": "+e.Message)
	}
	if len(parts) == 0 {
		return "未知错误"
	}
	return strings.Join(parts, "；")
}
