package caddyrender

import (
	"strings"
	"testing"

	"github.com/JiangBeta/gatebox/internal/objects"
)

func obj(kind, id string, spec map[string]any) objects.Object {
	return objects.Object{Kind: kind, ID: id, Key: id, Spec: spec, Status: map[string]any{}}
}

func domainObj(id, name string) objects.Object {
	o := obj("domain", id, map[string]any{"name": name})
	o.Key = name
	return o
}

func baseInput() Input {
	return Input{
		Entrypoints: []objects.Object{
			obj("entrypoint", "https", map[string]any{
				"protocol": "https", "ports": []any{443.0}, "network": "tcp", "enabled": true}),
			obj("entrypoint", "http", map[string]any{
				"protocol": "http", "ports": []any{80.0}, "network": "tcp", "enabled": true}),
			obj("entrypoint", "tcp", map[string]any{
				"protocol": "tcp", "ports": []any{22.0}, "network": "tcp", "enabled": true}),
		},
		Domains: []objects.Object{
			// id = slug(name)，key = 显示用的域名本身
			domainObj("neob-cn", "neob.cn"),
			domainObj("old-cn", "old.cn"),
		},
	}
}

type fakeRes map[string]string

func (f fakeRes) PasswordHash(id string) (string, error) {
	h, ok := f[id]
	if !ok {
		return "", errNoHash
	}
	return h, nil
}

var errNoHash = errStr("用户未设置密码")

type errStr string

func (e errStr) Error() string { return string(e) }

func TestRenderBasicRoute(t *testing.T) {
	in := baseInput()
	in.Routes = []objects.Object{
		obj("route", "aria", map[string]any{
			"name": "aria", "subdomain": "aria", "roots": []any{"neob-cn"},
			"entrypoint": "https", "port": 443.0, "tls": "auto", "enabled": true,
			"backend": map[string]any{"host": "192.168.1.20", "port": 6880.0},
		}),
	}
	got, err := Render(in, Options{DataDir: "/data"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 站点地址 = subdomain.根域名
	if !strings.Contains(got.Caddyfile, "aria.neob.cn {") {
		t.Errorf("站点地址异常:\n%s", got.Caddyfile)
	}
	if !strings.Contains(got.Caddyfile, "reverse_proxy 192.168.1.20:6880") {
		t.Errorf("上游异常:\n%s", got.Caddyfile)
	}
	if !strings.Contains(got.Caddyfile, "access.log") || !strings.Contains(got.Caddyfile, "roll_keep 5") {
		t.Errorf("全局块缺 access log:\n%s", got.Caddyfile)
	}
	if len(got.SiteAddrs) != 1 || got.SiteAddrs[0] != "aria.neob.cn" {
		t.Errorf("SiteAddrs: %v", got.SiteAddrs)
	}
}

func TestRenderHttpAndPortOverride(t *testing.T) {
	in := baseInput()
	in.Routes = []objects.Object{
		obj("route", "plain", map[string]any{
			"name": "plain", "subdomain": "", "roots": []any{"neob-cn"},
			"entrypoint": "http", "tls": "off", "enabled": true,
			"backend": map[string]any{"host": "h", "port": 1.0},
		}),
		obj("route", "alt", map[string]any{
			"name": "alt", "subdomain": "alt", "roots": []any{"neob-cn"},
			"entrypoint": "https", "port": 9443.0, "tls": "auto", "enabled": true,
			"backend": map[string]any{"host": "h", "port": 2.0},
		}),
	}
	got, err := Render(in, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// http 入口 → 显式 http:// 且端口 80 是默认值，不带端口
	if !strings.Contains(got.Caddyfile, "http://neob.cn {") {
		t.Errorf("http 站点地址异常:\n%s", got.Caddyfile)
	}
	// 非默认端口 → 显式 :9443
	if !strings.Contains(got.Caddyfile, "alt.neob.cn:9443 {") {
		t.Errorf("端口覆盖异常:\n%s", got.Caddyfile)
	}
	// tls off → 显式 tls internal
	if !strings.Contains(got.Caddyfile, "tls internal") {
		t.Errorf("tls off 应写 tls internal:\n%s", got.Caddyfile)
	}
}

func TestRenderMiddlewares(t *testing.T) {
	in := baseInput()
	in.Middlewares = []objects.Object{
		obj("middleware", "gzip", map[string]any{"name": "GZIP", "type": "encode", "formats": []any{"gzip", "zstd"}}),
		obj("middleware", "ws", map[string]any{"name": "WS", "type": "websocket"}),
		obj("middleware", "hdr", map[string]any{"name": "HDR", "type": "headers",
			"set": []any{"X-Frame-Options=DENY", "X-Powered-By="}, "delete": []any{"Server"}}),
		obj("middleware", "rw", map[string]any{"name": "RW", "type": "rewrite", "uri": "/old /new"}),
		obj("middleware", "raw", map[string]any{"name": "RAW", "type": "code",
			"code": "header {\n\tX-Custom yes\n}"}),
	}
	in.Users = []objects.Object{obj("user", "admin", map[string]any{"name": "admin", "enabled": true})}
	in.Routes = []objects.Object{
		obj("route", "aria", map[string]any{
			"name": "aria", "subdomain": "aria", "roots": []any{"neob-cn"},
			"entrypoint": "https", "tls": "auto", "enabled": true,
			"middlewares": []any{"gzip", "ws", "hdr", "rw", "raw"},
			"backend":     map[string]any{"host": "h", "port": 1.0},
		}),
	}
	got, err := Render(in, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"encode gzip zstd",
		"header {",
		"\tX-Frame-Options DENY",
		"\tX-Powered-By",
		"\tServer-",
		"rewrite /old /new",
		"\tX-Custom yes",
	} {
		if !strings.Contains(got.Caddyfile, want) {
			t.Errorf("缺 %q:\n%s", want, got.Caddyfile)
		}
	}
	// websocket 不产出指令
	if strings.Contains(got.Caddyfile, "websocket") {
		t.Errorf("websocket 不该产出指令:\n%s", got.Caddyfile)
	}
}

func TestRenderBasicAuthResolvesHash(t *testing.T) {
	in := baseInput()
	in.Users = []objects.Object{obj("user", "admin", map[string]any{"name": "admin", "enabled": true})}
	in.Middlewares = []objects.Object{
		obj("middleware", "auth", map[string]any{"name": "auth", "type": "basic_auth", "users": []any{"admin"}}),
	}
	in.Routes = []objects.Object{
		obj("route", "aria", map[string]any{
			"name": "aria", "subdomain": "aria", "roots": []any{"neob-cn"},
			"entrypoint": "https", "tls": "auto", "enabled": true,
			"middlewares": []any{"auth"},
			"backend":     map[string]any{"host": "h", "port": 1.0},
		}),
	}
	got, err := Render(in, Options{}, fakeRes{"admin": "$2y$14$FAKEHASH"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Caddyfile, "basic_auth {") || !strings.Contains(got.Caddyfile, "\tadmin $2y$14$FAKEHASH") {
		t.Errorf("basic_auth 渲染异常:\n%s", got.Caddyfile)
	}
}

func TestRenderErrors(t *testing.T) {
	cases := map[string]struct {
		mutate func(*Input)
		res    Resolver
		want   string
	}{
		"中间件不存在": {
			mutate: func(in *Input) {
				in.Routes[0].Spec["middlewares"] = []any{"ghost"}
			},
			want: "中间件 \"ghost\" 不存在",
		},
		"协议未安装": {
			mutate: func(in *Input) {
				in.Routes[0].Spec["entrypoint"] = "tcp"
			},
			want: "组件未安装",
		},
		"根域名不存在": {
			mutate: func(in *Input) {
				in.Routes[0].Spec["roots"] = []any{"ghost-cn"}
			},
			want: "根域名 \"ghost-cn\" 不存在",
		},
		"未知中间件类型": {
			mutate: func(in *Input) {
				in.Middlewares = []objects.Object{obj("middleware", "x", map[string]any{"name": "x", "type": "brand_new"})}
				in.Routes[0].Spec["middlewares"] = []any{"x"}
			},
			want: "尚无渲染实现",
		},
		"rewrite.body 未支持": {
			mutate: func(in *Input) {
				in.Middlewares = []objects.Object{obj("middleware", "x",
					map[string]any{"name": "x", "type": "rewrite", "body": map[string]any{"method": "replace"}})}
				in.Routes[0].Spec["middlewares"] = []any{"x"}
			},
			want: "rewrite.body 尚未支持",
		},
		"响应头缺等号": {
			mutate: func(in *Input) {
				in.Middlewares = []objects.Object{obj("middleware", "x",
					map[string]any{"name": "x", "type": "headers", "set": []any{"NoEquals"}})}
				in.Routes[0].Spec["middlewares"] = []any{"x"}
			},
			want: "缺少 '='",
		},
		"basic_auth 无用户": {
			mutate: func(in *Input) {
				in.Middlewares = []objects.Object{obj("middleware", "x",
					map[string]any{"name": "x", "type": "basic_auth"})}
				in.Routes[0].Spec["middlewares"] = []any{"x"}
			},
			want: "没有勾选用户",
		},
		"basic_auth 缺哈希": {
			mutate: func(in *Input) {
				in.Users = []objects.Object{obj("user", "admin", map[string]any{"name": "admin"})}
				in.Middlewares = []objects.Object{obj("middleware", "x",
					map[string]any{"name": "x", "type": "basic_auth", "users": []any{"admin"}})}
				in.Routes[0].Spec["middlewares"] = []any{"x"}
			},
			res:  fakeRes{},
			want: "未设置密码",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := baseInput()
			in.Routes = []objects.Object{
				obj("route", "aria", map[string]any{
					"name": "aria", "subdomain": "aria", "roots": []any{"neob-cn"},
					"entrypoint": "https", "tls": "auto", "enabled": true,
					"backend": map[string]any{"host": "h", "port": 1.0},
				}),
			}
			tc.mutate(&in)
			_, err := Render(in, Options{}, tc.res)
			if err == nil {
				t.Fatalf("应报错，实际 nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息应含 %q，实际 %v", tc.want, err)
			}
		})
	}
}

func TestRenderSkipsDisabledAndDeterministicOrder(t *testing.T) {
	in := baseInput()
	in.Routes = []objects.Object{
		obj("route", "zeta", map[string]any{
			"name": "zeta", "subdomain": "z", "roots": []any{"neob-cn"},
			"entrypoint": "https", "enabled": true, "backend": map[string]any{"host": "h", "port": 1.0}}),
		obj("route", "off", map[string]any{
			"name": "off", "subdomain": "o", "roots": []any{"neob-cn"},
			"entrypoint": "https", "enabled": false, "backend": map[string]any{"host": "h", "port": 1.0}}),
		obj("route", "alpha", map[string]any{
			"name": "alpha", "subdomain": "a", "roots": []any{"neob-cn"},
			"entrypoint": "https", "enabled": true, "backend": map[string]any{"host": "h", "port": 1.0}}),
	}
	got, err := Render(in, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skipped) != 1 || got.Skipped[0].RouteID != "off" {
		t.Errorf("停用路由应被跳过并记录: %+v", got.Skipped)
	}
	// 输出按 key 升序（alpha, zeta）
	ia := strings.Index(got.Caddyfile, "a.neob.cn {")
	iz := strings.Index(got.Caddyfile, "z.neob.cn {")
	if ia < 0 || iz < 0 || ia > iz {
		t.Errorf("输出应按 key 升序:\n%s", got.Caddyfile)
	}
}

func TestRenderL4WithoutDomain(t *testing.T) {
	in := baseInput()
	// 端口型监听（无域名）
	in.Routes = []objects.Object{
		obj("route", "portonly", map[string]any{
			"name": "portonly", "entrypoint": "https", "port": 9999.0, "enabled": true,
			"backend": map[string]any{"host": "h", "port": 1.0}}),
	}
	got, err := Render(in, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Caddyfile, ":9999 {") {
		t.Errorf("无域名应退化为端口监听:\n%s", got.Caddyfile)
	}

	// 端口继承入口点主端口
	in.Routes[0].Spec["port"] = nil
	delete(in.Routes[0].Spec, "port")
	got, err = Render(in, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Caddyfile, ":443 {") {
		t.Errorf("无域名无端口时应继承入口点主端口:\n%s", got.Caddyfile)
	}

	// 入口点也没有端口 → 无法确定监听地址
	in.Entrypoints[0] = obj("entrypoint", "https", map[string]any{
		"protocol": "https", "network": "tcp", "enabled": true})
	if _, err := Render(in, Options{}, nil); err == nil ||
		!strings.Contains(err.Error(), "无法确定监听地址") {
		t.Errorf("应报无法确定监听地址，实际 %v", err)
	}
}

func TestRenderEmptyRoutesStillHasGlobalBlock(t *testing.T) {
	got, err := Render(baseInput(), Options{DataDir: "/data", HTTPPort: 8080, HTTPSPort: 8443}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"http_port 8080", "https_port 8443", "log {"} {
		if !strings.Contains(got.Caddyfile, want) {
			t.Errorf("全局块缺 %q:\n%s", want, got.Caddyfile)
		}
	}
}

func TestRenderGlobalSnippets(t *testing.T) {
	got, err := Render(baseInput(), Options{GlobalSnippets: []string{"l4 {\n\tfoo\n}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Caddyfile, "\tl4 {") || !strings.Contains(got.Caddyfile, "\t\tfoo") {
		t.Errorf("全局片段未按一层缩进写入:\n%s", got.Caddyfile)
	}
}

// 服务类型必须决定上游指令形态：file_server 没有上游，
// 若按 route.backend 渲染会把静态站点变成 reverse_proxy 127.0.0.1:1。
func TestRenderFileServerRoute(t *testing.T) {
	in := baseInput()
	in.Services = []objects.Object{
		obj("service", "www", map[string]any{"name": "www", "type": "file_server", "root": "/var/www"}),
	}
	in.Routes = []objects.Object{
		obj("route", "www", map[string]any{
			"name": "www", "roots": []any{"neob-cn"}, "entrypoint": "https",
			"tls": "auto", "enabled": true, "service": "www",
			"backend": map[string]any{"host": "127.0.0.1", "port": 1.0},
		}),
	}
	res, err := Render(in, Options{}, fakeRes{})
	if err != nil {
		t.Fatalf("渲染 file_server 失败: %v", err)
	}
	if !strings.Contains(res.Caddyfile, "root /var/www") {
		t.Errorf("应渲染 root 指令:\n%s", res.Caddyfile)
	}
	if strings.Contains(res.Caddyfile, "reverse_proxy 127.0.0.1:1") {
		t.Errorf("不该把静态站点渲染成反向代理占位后端:\n%s", res.Caddyfile)
	}
	if strings.Contains(res.Caddyfile, "file_server browse") {
		t.Errorf("browse 默认 false，不该渲染 browse:\n%s", res.Caddyfile)
	}
}

func TestRenderFileServerBrowse(t *testing.T) {
	in := baseInput()
	in.Services = []objects.Object{
		obj("service", "www", map[string]any{"type": "file_server", "root": "/srv", "browse": true}),
	}
	in.Routes = []objects.Object{
		obj("route", "www", map[string]any{
			"name": "www", "roots": []any{"neob-cn"}, "entrypoint": "https",
			"enabled": true, "service": "www",
			"backend": map[string]any{"host": "127.0.0.1", "port": 1.0},
		}),
	}
	res, err := Render(in, Options{}, fakeRes{})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(res.Caddyfile, "file_server browse") {
		t.Errorf("browse=true 应渲染 file_server browse:\n%s", res.Caddyfile)
	}
}

func TestRenderRespondAndRedirect(t *testing.T) {
	in := baseInput()
	in.Services = []objects.Object{
		obj("service", "maintenance", map[string]any{"type": "respond", "body": "维护中", "status": 503.0}),
		obj("service", "old", map[string]any{"type": "redirect", "to": "https://neob.cn"}),
	}
	in.Routes = []objects.Object{
		obj("route", "maint", map[string]any{
			"name": "maint", "roots": []any{"neob-cn"}, "entrypoint": "https",
			"enabled": true, "service": "maintenance",
			"backend": map[string]any{"host": "127.0.0.1", "port": 1.0},
		}),
		obj("route", "go", map[string]any{
			// 必须换根域名：两条路由不能抢同一个站点地址（同址会被渲染器拒绝）。
			"name": "go", "roots": []any{"old-cn"}, "entrypoint": "https",
			"enabled": true, "service": "old",
			"backend": map[string]any{"host": "127.0.0.1", "port": 1.0},
		}),
	}
	res, err := Render(in, Options{}, fakeRes{})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(res.Caddyfile, `respond /503 "维护中"`) {
		t.Errorf("respond 应带状态码:\n%s", res.Caddyfile)
	}
	if !strings.Contains(res.Caddyfile, "redir https://neob.cn") {
		t.Errorf("redirect 应渲染 redir:\n%s", res.Caddyfile)
	}
}

// 类型目录已列出但渲染未实现的类型（如 l4_proxy）必须报错，
// 让上层把 status.state 写成「错误」，而不是悄悄渲染成反向代理。
func TestRenderUnimplementedServiceType(t *testing.T) {
	in := baseInput()
	in.Services = []objects.Object{
		obj("service", "l4", map[string]any{"type": "l4_proxy"}),
	}
	in.Routes = []objects.Object{
		obj("route", "l4", map[string]any{
			"name": "l4", "roots": []any{"neob-cn"}, "entrypoint": "https",
			"enabled": true, "service": "l4",
			"backend": map[string]any{"host": "10.0.0.1", "port": 9000.0},
		}),
	}
	if _, err := Render(in, Options{}, fakeRes{}); err == nil {
		t.Fatal("未实现的服务类型应报错，而不是渲染成反向代理")
	}
}

// 路由引用的服务缺失 → 报错，不静默降级。
func TestRenderMissingServiceRef(t *testing.T) {
	in := baseInput()
	in.Routes = []objects.Object{
		obj("route", "x", map[string]any{
			"name": "x", "roots": []any{"neob-cn"}, "entrypoint": "https",
			"enabled": true, "service": "gone",
			"backend": map[string]any{"host": "10.0.0.1", "port": 9000.0},
		}),
	}
	if _, err := Render(in, Options{}, fakeRes{}); err == nil {
		t.Fatal("引用不存在的服务应报错")
	}
}

// TestDuplicateSiteAddrRejected 同址重复必须在渲染期就报清楚是哪两条路由撞了。
// 回归背景：不拦的话只有 caddy validate 会说
// 「ambiguous site definition: http://x:8081」，用户无从下手。
func TestDuplicateSiteAddrRejected(t *testing.T) {
	ep := objects.Object{ID: "http", Kind: "entrypoint", Spec: map[string]any{
		"protocol": "http", "ports": []any{8081}, "network": "tcp",
	}}
	dom := objects.Object{ID: "ex", Key: "ex.example", Kind: "domain", Spec: map[string]any{"name": "ex.example"}}
	mk := func(id string) objects.Object {
		return objects.Object{ID: id, Kind: "route", Spec: map[string]any{
			"name": id, "enabled": true, "entrypoint": "http", "roots": []string{"ex"},
			"backend": map[string]any{"host": "127.0.0.1", "port": 1},
		}}
	}
	_, err := Render(Input{
		Entrypoints: []objects.Object{ep},
		Domains:     []objects.Object{dom},
		Routes:      []objects.Object{mk("r1"), mk("r2")},
	}, Options{}, nil)
	if err == nil {
		t.Fatal("两条路由同址应报错")
	}
	msg := err.Error()
	for _, want := range []string{"r1", "r2", "ex.example"} {
		if !strings.Contains(msg, want) {
			t.Errorf("错误应点出 %s，实际 %q", want, msg)
		}
	}
}
