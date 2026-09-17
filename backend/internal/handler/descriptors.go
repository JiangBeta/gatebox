package handler

import (
	"net/http"
	"sort"

	"github.com/JiangBeta/gatebox/internal/component"
	"github.com/JiangBeta/gatebox/internal/plugin"
)

// descriptorDTO 是 /api/v1/descriptors 的对外视图（camelCase，ADR-041 §5）。
//
// 独立于 component.Descriptor（后者无 json tag，保持 /api/v1/components 零回归）。
type descriptorDTO struct {
	ID             string                  `json:"id"`
	Name           string                  `json:"name"`
	Summary        string                  `json:"summary,omitempty"`
	Kind           string                  `json:"kind"`
	Tier           string                  `json:"tier"`
	Tags           []string                `json:"tags,omitempty"`
	Provision      string                  `json:"provision"`
	Runtime        string                  `json:"runtime"`
	Upgrade        string                  `json:"upgrade"`
	Capabilities   []string                `json:"capabilities,omitempty"`
	DefaultEnabled bool                    `json:"defaultEnabled,omitempty"`
	Removable      bool                    `json:"removable,omitempty"`
	Bundled        bool                    `json:"bundled,omitempty"`
	Functions      []component.Function    `json:"functions,omitempty"`
	Config         []component.ConfigField `json:"config,omitempty"`
	Consumes       []component.InfoPort    `json:"consumes,omitempty"`
	Produces       []component.InfoPort    `json:"produces,omitempty"`
	Observable     component.Observability `json:"observable"`
}

func toDescriptorDTO(d component.Descriptor) descriptorDTO {
	return descriptorDTO{
		ID:             d.ID,
		Name:           d.Name,
		Summary:        d.Summary,
		Kind:           string(d.Kind),
		Tier:           d.Tier,
		Tags:           d.Tags,
		Provision:      d.Provision,
		Runtime:        d.Runtime,
		Upgrade:        d.Upgrade,
		Capabilities:   d.Capabilities,
		DefaultEnabled: d.DefaultEnabled,
		Removable:      d.Removable,
		Bundled:        d.Bundled,
		Functions:      d.Functions,
		Config:         d.Config,
		Consumes:       d.Consumes,
		Produces:       d.Produces,
		Observable:     d.Observable,
	}
}

// allDescriptors 汇总内建组件与已启用插件的描述符（稳定排序）。
func allDescriptors(core *component.CoreRegistry, mgr *plugin.Manager) []component.Descriptor {
	descs := make([]component.Descriptor, 0)
	if core != nil {
		descs = append(descs, core.Descriptors()...)
	}
	if mgr != nil {
		descs = append(descs, mgr.Descriptors()...)
	}
	sort.Slice(descs, func(i, j int) bool { return descs[i].ID < descs[j].ID })
	return descs
}

// RegisterDescriptors 注册 v4 的组件描述符端点（前端派生引擎与功能地图的数据源）。
func RegisterDescriptors(mux *http.ServeMux, core *component.CoreRegistry, mgr *plugin.Manager) {
	mux.HandleFunc("GET /api/v1/descriptors", func(w http.ResponseWriter, _ *http.Request) {
		descs := allDescriptors(core, mgr)
		out := make([]descriptorDTO, 0, len(descs))
		for _, d := range descs {
			out = append(out, toDescriptorDTO(d))
		}
		writeJSON(w, http.StatusOK, out)
	})
}
