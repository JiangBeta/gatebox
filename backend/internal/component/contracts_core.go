package component

// 内建组件的功能声明与四契约（ADR-041 §4.1）。
//
// 只补描述（静态），不改变生命周期与探测逻辑。

func fn(id, label string) Function { return Function{ID: id, Label: label} }

// applyCoreContracts 为内建组件补齐 v4 功能与四契约。
func applyCoreContracts(cores []core) {
	for i := range cores {
		switch cores[i].desc.ID {
		case "caddy":
			cores[i].desc.Functions = []Function{
				fn("reverse-proxy", "反向代理"),
				fn("static-serve", "静态文件服务"),
				fn("http-listen", "HTTP/HTTPS 接入"),
			}
			cores[i].desc.Consumes = []InfoPort{
				{Info: InfoService, Effect: []EffectAction{EffectUpdateConfig, EffectHotReload}},
				{Info: InfoPortBinding, Effect: []EffectAction{EffectUpdateConfig, EffectHotReload}},
				{Info: InfoFragment, Effect: []EffectAction{EffectUpdateConfig, EffectHotReload}},
				{Info: InfoVariable, Effect: []EffectAction{EffectUpdateConfig, EffectHotReload}},
				{Info: InfoDomain, Effect: []EffectAction{EffectUpdateConfig, EffectHotReload}},
				{Info: InfoCert, Effect: []EffectAction{EffectUpdateConfig, EffectHotReload}},
			}
			cores[i].desc.Observable = Observability{State: true, Logs: true}
		case "acme":
			cores[i].desc.Functions = []Function{
				fn("cert-issue", "证书签发/续期"),
			}
			cores[i].desc.Consumes = []InfoPort{
				{Info: InfoDomain, Effect: []EffectAction{EffectReissue, EffectUpdateConfig}},
				{Info: InfoCredential, Effect: []EffectAction{EffectReissue, EffectUpdateConfig}},
			}
			cores[i].desc.Produces = []InfoPort{
				{Info: InfoCert},
			}
			cores[i].desc.Observable = Observability{State: true, Logs: true}
		case "docker":
			cores[i].desc.Functions = []Function{
				fn("container-runtime", "容器运行时"),
				fn("label-publish", "容器标签产出"),
			}
			cores[i].desc.Consumes = []InfoPort{
				{Info: InfoVariable, Effect: []EffectAction{EffectUpdateConfig}},
				{Info: InfoPortBinding, Effect: []EffectAction{EffectUpdateConfig}},
			}
			cores[i].desc.Produces = []InfoPort{
				{Info: InfoLabel},
				{Info: InfoService},
			}
			cores[i].desc.Observable = Observability{State: true, Logs: true}
		}
	}
}
