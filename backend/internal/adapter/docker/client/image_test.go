package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSplitImageRef(t *testing.T) {
	cases := []struct {
		ref       string
		name, tag string
	}{
		{"nginx", "nginx", ""},
		{"nginx:latest", "nginx", "latest"},
		{"library/nginx:1.25", "library/nginx", "1.25"},
		{"ghcr.io/wikid82/charon:latest", "ghcr.io/wikid82/charon", "latest"},
		{"traefik:v3.7.10", "traefik", "v3.7.10"},
		// 关键边界:registry 端口里的冒号不是 tag 分隔符
		{"localhost:5000/nginx", "localhost:5000/nginx", ""},
		{"localhost:5000/nginx:v1", "localhost:5000/nginx", "v1"},
		{"registry.example.com:443/team/app:dev", "registry.example.com:443/team/app", "dev"},
		// digest 形式
		{"nginx@sha256:abc123", "nginx", "sha256:abc123"},
	}
	for _, tc := range cases {
		name, tag := splitImageRef(tc.ref)
		if name != tc.name || tag != tc.tag {
			t.Errorf("splitImageRef(%q) = (%q, %q), want (%q, %q)", tc.ref, name, tag, tc.name, tc.tag)
		}
	}
}

func TestRegistryAuthHeader(t *testing.T) {
	a := &RegistryAuth{Username: "u", Password: "p@ss/w+rd", ServerAddress: "registry.example.com"}
	h, err := a.header()
	if err != nil {
		t.Fatalf("header: %v", err)
	}

	// 必须是 base64url(JSON) —— 用标准 base64 会因 +/ 字符在 header 中出问题
	raw, err := base64.URLEncoding.DecodeString(h)
	if err != nil {
		t.Fatalf("不是合法的 base64url: %v", err)
	}
	var back RegistryAuth
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("解码后不是合法 JSON: %v", err)
	}
	if back.Username != "u" || back.Password != "p@ss/w+rd" || back.ServerAddress != "registry.example.com" {
		t.Errorf("往返后 = %+v", back)
	}

	// nil 认证 = 匿名,不应产生头
	var nilAuth *RegistryAuth
	if h, err := nilAuth.header(); err != nil || h != "" {
		t.Errorf("nil 认证应返回空串, got (%q, %v)", h, err)
	}
}

func TestPullImage_QueryAndAuthHeader(t *testing.T) {
	var (
		gotQuery url.Values
		gotAuth  string
	)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		gotAuth = r.Header.Get("X-Registry-Auth")
		w.Write([]byte(`{"status":"Status: Downloaded newer image for nginx:1.25"}`))
	})

	rc, err := c.PullImage(context.Background(), "nginx:1.25", "linux/arm64",
		&RegistryAuth{Username: "u", Password: "p"})
	if err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	defer rc.Close()

	if got := gotQuery.Get("fromImage"); got != "nginx" {
		t.Errorf("fromImage = %q, want nginx", got)
	}
	if got := gotQuery.Get("tag"); got != "1.25" {
		t.Errorf("tag = %q, want 1.25", got)
	}
	if got := gotQuery.Get("platform"); got != "linux/arm64" {
		t.Errorf("platform = %q", got)
	}
	if gotAuth == "" {
		t.Error("未发送 X-Registry-Auth 头")
	}
}

func TestPullImage_AnonymousOmitsAuthHeader(t *testing.T) {
	var hasAuth bool
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, hasAuth = r.Header["X-Registry-Auth"]
		w.Write([]byte(`{}`))
	})
	rc, err := c.PullImage(context.Background(), "nginx", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rc.Close()
	if hasAuth {
		t.Error("匿名拉取不应发送 X-Registry-Auth 头")
	}
}

// --- 进度聚合 ---

// realPullStream 是 docker pull 的典型事件序列(两层,一层已存在)。
const realPullStream = `
{"status":"Pulling from library/nginx","id":"1.25"}
{"status":"Pulling fs layer","progressDetail":{},"id":"aaaa"}
{"status":"Pulling fs layer","progressDetail":{},"id":"bbbb"}
{"status":"Already exists","progressDetail":{},"id":"cccc"}
{"status":"Waiting","progressDetail":{},"id":"bbbb"}
{"status":"Downloading","progressDetail":{"current":2500,"total":10000},"progress":"[==>  ]","id":"aaaa"}
{"status":"Downloading","progressDetail":{"current":10000,"total":10000},"progress":"[====]","id":"aaaa"}
{"status":"Verifying Checksum","progressDetail":{},"id":"aaaa"}
{"status":"Download complete","progressDetail":{},"id":"aaaa"}
{"status":"Downloading","progressDetail":{"current":10000,"total":10000},"id":"bbbb"}
{"status":"Download complete","progressDetail":{},"id":"bbbb"}
{"status":"Extracting","progressDetail":{"current":5000,"total":10000},"id":"aaaa"}
{"status":"Extracting","progressDetail":{"current":10000,"total":10000},"id":"aaaa"}
{"status":"Pull complete","progressDetail":{},"id":"aaaa"}
{"status":"Extracting","progressDetail":{"current":10000,"total":10000},"id":"bbbb"}
{"status":"Pull complete","progressDetail":{},"id":"bbbb"}
{"status":"Digest: sha256:deadbeef"}
{"status":"Status: Downloaded newer image for nginx:1.25"}
`

func TestDecodePullStream_RealSequence(t *testing.T) {
	var (
		updates  int
		last     PullProgress
		maxSeen  float64
		monotone = true
	)
	err := DecodePullStream(context.Background(), strings.NewReader(realPullStream), func(p PullProgress) error {
		updates++
		if p.Percent < maxSeen-0.001 {
			monotone = false
			t.Errorf("进度回退:第 %d 次更新 %.2f%% < 之前的 %.2f%%", updates, p.Percent, maxSeen)
		}
		if p.Percent > maxSeen {
			maxSeen = p.Percent
		}
		if p.Percent < 0 || p.Percent > 100 {
			t.Errorf("进度越界: %.2f%%", p.Percent)
		}
		last = p
		return nil
	})
	if err != nil {
		t.Fatalf("DecodePullStream: %v", err)
	}

	if updates == 0 {
		t.Fatal("没有产生任何进度更新")
	}
	if !monotone {
		t.Error("进度非单调递增 —— UI 上会看到进度条倒退")
	}
	if !last.Done {
		t.Error("收到 Status: 行后应标记为完成")
	}
	if math.Abs(last.Percent-100) > 0.001 {
		t.Errorf("最终进度 = %.2f%%, want 100%%", last.Percent)
	}
	if len(last.Layers) != 3 {
		t.Errorf("层数 = %d, want 3", len(last.Layers))
	}
	for _, l := range last.Layers {
		if l.Phase != "complete" {
			t.Errorf("层 %s 最终阶段 = %s, want complete", l.ID, l.Phase)
		}
	}
}

// TestProgressAggregator_ByteWeighting 验证 80/20 的下载/解压权重。
func TestProgressAggregator_ByteWeighting(t *testing.T) {
	a := NewProgressAggregator()
	a.Apply(PullMessage{Status: "Pulling fs layer", ID: "x"})

	// 下载到一半 → 0.5 × 0.8 = 40%
	a.Apply(PullMessage{Status: "Downloading", ID: "x", ProgressDetail: &ProgressDetail{Current: 500, Total: 1000}})
	if p := a.Progress(); math.Abs(p.Percent-40) > 0.01 {
		t.Errorf("下载 50%% 时总进度 = %.2f%%, want 40%%", p.Percent)
	}

	// 下载完成 → 80%
	a.Apply(PullMessage{Status: "Download complete", ID: "x"})
	if p := a.Progress(); math.Abs(p.Percent-80) > 0.01 {
		t.Errorf("下载完成时总进度 = %.2f%%, want 80%%", p.Percent)
	}

	// 解压到一半 → 80% + 0.5×20% = 90%
	a.Apply(PullMessage{Status: "Extracting", ID: "x", ProgressDetail: &ProgressDetail{Current: 500, Total: 1000}})
	if p := a.Progress(); math.Abs(p.Percent-90) > 0.01 {
		t.Errorf("解压 50%% 时总进度 = %.2f%%, want 90%%", p.Percent)
	}

	// 完成 → 100%
	a.Apply(PullMessage{Status: "Pull complete", ID: "x"})
	if p := a.Progress(); math.Abs(p.Percent-100) > 0.01 {
		t.Errorf("完成时总进度 = %.2f%%, want 100%%", p.Percent)
	}
}

// TestProgressAggregator_WeightedByLayerSize 大层应比小层占更大权重。
func TestProgressAggregator_WeightedByLayerSize(t *testing.T) {
	a := NewProgressAggregator()
	// big 层 9000 字节,small 层 1000 字节
	a.Apply(PullMessage{Status: "Downloading", ID: "big", ProgressDetail: &ProgressDetail{Current: 0, Total: 9000}})
	a.Apply(PullMessage{Status: "Downloading", ID: "small", ProgressDetail: &ProgressDetail{Current: 0, Total: 1000}})

	// 小层全部完成 → 只应贡献 10% 的权重
	a.Apply(PullMessage{Status: "Pull complete", ID: "small"})
	p := a.Progress()
	if !p.ByteWeighted {
		t.Fatal("应处于字节加权模式")
	}
	if math.Abs(p.Percent-10) > 0.01 {
		t.Errorf("小层完成后总进度 = %.2f%%, want 10%%(按字节加权)", p.Percent)
	}
}

// TestProgressAggregator_AlreadyExistsDoesNotDegrade ——
// "Already exists" 的层没有下载量,但它确定已完成,不应把整体拖入降级模式。
func TestProgressAggregator_AlreadyExists(t *testing.T) {
	a := NewProgressAggregator()
	a.Apply(PullMessage{Status: "Already exists", ID: "cached"})
	a.Apply(PullMessage{Status: "Downloading", ID: "new", ProgressDetail: &ProgressDetail{Current: 500, Total: 1000}})

	p := a.Progress()
	if !p.ByteWeighted {
		t.Error("Already exists 的层不应触发降级为层数计数")
	}
	// 仅 new 层参与加权:0.5 × 0.8 = 40%
	if math.Abs(p.Percent-40) > 0.01 {
		t.Errorf("总进度 = %.2f%%, want 40%%", p.Percent)
	}
}

// TestProgressAggregator_DegradesOnUnknownSize 总量未知时降级为层数计数。
func TestProgressAggregator_DegradesOnUnknownSize(t *testing.T) {
	a := NewProgressAggregator()
	a.Apply(PullMessage{Status: "Pulling fs layer", ID: "a"})
	a.Apply(PullMessage{Status: "Pulling fs layer", ID: "b"})
	a.Apply(PullMessage{Status: "Pulling fs layer", ID: "c"})
	a.Apply(PullMessage{Status: "Pull complete", ID: "a"})

	p := a.Progress()
	if p.ByteWeighted {
		t.Error("存在总量未知的未完成层时,应降级为层数计数")
	}
	// 1/3 完成
	if math.Abs(p.Percent-100.0/3) > 0.01 {
		t.Errorf("降级模式进度 = %.2f%%, want %.2f%%", p.Percent, 100.0/3)
	}
}

func TestProgressAggregator_Error(t *testing.T) {
	a := NewProgressAggregator()
	a.Apply(PullMessage{Status: "Pulling from library/nope"})
	a.Apply(PullMessage{Error: "pull access denied for nope, repository does not exist"})

	p := a.Progress()
	if p.Err == nil {
		t.Fatal("应记录错误")
	}
	if !strings.Contains(p.Err.Error(), "pull access denied") {
		t.Errorf("错误信息 = %v", p.Err)
	}
	if p.Done {
		t.Error("出错时不应标记为完成")
	}
}

func TestDecodePullStream_ErrorPropagates(t *testing.T) {
	stream := `{"status":"Pulling from library/nope"}
{"errorDetail":{"message":"manifest unknown"},"error":"manifest unknown"}`

	err := DecodePullStream(context.Background(), strings.NewReader(stream), func(PullProgress) error { return nil })
	if err == nil {
		t.Fatal("事件流中的错误应作为返回值传出")
	}
	if !strings.Contains(err.Error(), "manifest unknown") {
		t.Errorf("err = %v", err)
	}
}

func TestDecodePullStream_CallbackAborts(t *testing.T) {
	stop := context.Canceled
	err := DecodePullStream(context.Background(), strings.NewReader(realPullStream),
		func(PullProgress) error { return stop })
	if err != stop {
		t.Errorf("回调返回错误应中止解析, got %v", err)
	}
}

func TestDecodePullStream_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := DecodePullStream(ctx, strings.NewReader(realPullStream), func(PullProgress) error { return nil })
	if err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// --- 其余端点 ---

func TestListImages_Decode(t *testing.T) {
	const body = `[{
		"Id":"sha256:abc123",
		"RepoTags":["traefik:v3.7.10"],
		"RepoDigests":["traefik@sha256:deadbeef"],
		"Created":1755000000,
		"Size":52428800,
		"Containers":1
	}]`
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})
	list, err := c.ListImages(context.Background(), ListImagesOptions{})
	if err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("镜像数 = %d", len(list))
	}
	if got := list[0].Digest(); got != "sha256:deadbeef" {
		t.Errorf("Digest() = %q", got)
	}
	if list[0].CreatedAt().Unix() != 1755000000 {
		t.Errorf("CreatedAt() = %v", list[0].CreatedAt())
	}
}

func TestImageDigest_NoRepoDigests(t *testing.T) {
	img := &Image{RepoDigests: nil}
	if got := img.Digest(); got != "" {
		t.Errorf("无 RepoDigests 时 Digest() = %q, want 空串", got)
	}
}

func TestInspectImage_Platform(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Id":"sha256:x","Architecture":"arm64","Os":"linux","Variant":"v8"}`))
	})
	d, err := c.InspectImage(context.Background(), "nginx")
	if err != nil {
		t.Fatalf("InspectImage: %v", err)
	}
	if got := d.Platform(); got != "linux/arm64/v8" {
		t.Errorf("Platform() = %q, want linux/arm64/v8", got)
	}

	noVariant := &ImageDetail{Os: "linux", Architecture: "amd64"}
	if got := noVariant.Platform(); got != "linux/amd64" {
		t.Errorf("无 variant 时 Platform() = %q", got)
	}
}

func TestRemoveImage_InUseReturnsConflict(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"message":"conflict: unable to delete abc123 (must be forced) - image is being used by running container xyz"}`))
	})
	_, err := c.RemoveImage(context.Background(), "abc123", RemoveImageOptions{})
	if err == nil {
		t.Fatal("使用中的镜像应报错")
	}
	if !strings.Contains(err.Error(), "image is being used") {
		t.Errorf("err = %v", err)
	}
}

func TestPruneImages(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ImagesDeleted":[{"Deleted":"sha256:a"}],"SpaceReclaimed":1048576}`))
	})
	deleted, reclaimed, err := c.PruneImages(context.Background(), nil)
	if err != nil {
		t.Fatalf("PruneImages: %v", err)
	}
	if len(deleted) != 1 || deleted[0].Deleted != "sha256:a" {
		t.Errorf("deleted = %+v", deleted)
	}
	if reclaimed != 1048576 {
		t.Errorf("reclaimed = %d", reclaimed)
	}
}
