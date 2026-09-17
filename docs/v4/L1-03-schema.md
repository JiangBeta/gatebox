# L1-U3 · 配置契约与 schema 下发

> 上层：[architecture.md §2.2/§11](../architecture.md) · [ADR-041 §8/§10](../adr/ADR-041.md)
> 状态：设计稿（待评审）· 依赖：U1（`component.ConfigField`）

## 1. 目标与非目标

**目标**

1. 为每个**事实类别**（Service / Domain / Credential / Port / Variable / Fragment / Compose）声明**配置契约**（`ConfigField[]`）。
2. 暴露 `GET /api/v1/schema/{factKind}`，供前端派生引擎（U4）生成编辑表单。
3. 统一动态 schema：DNS 凭证字段由 `dns-provider` 能力提供，泛化 `ProviderField → ConfigField`。

**非目标**

- **不改编辑 API**：现有 REST（`POST /domains`、`PUT /gateway/services/{id}`…）不变；schema 只驱动 UI 生成。
- 不做 YAML 补全/lint（L1 只做 Form；YAML 双模仅限有文本源的事实，见 U4）。
- 不做条件字段的完整表达式系统（`type` 联动等，见开放问题）。

## 2. 概念边界

| 契约 | 载体 | 端点 | 用途 |
|---|---|---|---|
| **组件**配置契约 | `component.Descriptor.Config`（U1） | `/api/v1/descriptors` | 配置组件自身（少用） |
| **事实**配置契约 | `graph.FactKind.Schema`（本单元） | `/api/v1/schema/{factKind}` | 编辑事实（主用途：网关/域名/凭证…） |

> 二者共用同一 `ConfigField` 类型；本单元只新增「事实 schema 注册 + 端点」。

## 3. 类型补充（对 U1 的修订）

`ConfigType` 增加 `password`（凭证必需）：

```go
const ConfigPassword ConfigType = "password" // 掩码输入，不回显明文
```

## 4. schema 注册表

配置契约与事实注册表（U2）**同处声明**（`internal/graph/facts.go`），每个 `FactKind` 增加 `Schema []component.ConfigField`：

```go
type FactKind struct {
    Kind       string
    Info       component.InfoType
    Persisted  bool
    Source     string
    References []FactReference
    Schema     []component.ConfigField `json:"schema"`   // 【U3 新增】
    UIHint     string                  `json:"uiHint"`   // 【U3 新增】自定义渲染提示（见 §7）
}
```

## 5. 各事实 schema 草案（L1）

**service**（`uiHint: gateway-service`）

| key | label | type | 备注 |
|---|---|---|---|
| `appId` | 归属应用 | reference → app | 必填 |
| `name` | 服务名 | text | 必填 |
| `type` | 代理类型 | select：`reverse_proxy` / `file_server` | 必填 |
| `domains` | 发布域名 | array(object)：`protocol`(select https/http) / `subdomain`(text) / `rootDomain`(reference→domain) / `customPort`(switch) / `port`(number) | 必填 |
| `upstream` | 后端地址 | array(text) | reverse_proxy 必填 |
| `upstreamProto` | 后端协议 | select：`http` / `https` | 高级 |
| `root` | 静态根目录 | text | file_server 用，高级 |
| `healthUri` | 健康检查 | text | 高级 |
| `fragmentIds` | Caddy 片段 | array(reference → fragment) | 高级 |
| `enabled` | 启用 | switch | |

**domain**：`name`(text 必填) · `credentialId`(reference→credential) · `remark`(text 高级)

**port**：`protocol`(text 必填) · `ports`(array number) · `network`(select tcp/udp/tcp+udp) · `enabled`(switch)

**variable**：`key`(text 必填，validator `ValidVariableKey`) · `value`(text) · `secret`(switch 高级)

**fragment**（`uiHint: fragment-code`）：`name`(text) · `description`(textarea 高级) · `code`(textarea，**Caddyfile 语法**) · `defaultEnabled`(switch) · `defaultHidden`(switch)

**compose**（`uiHint: compose-editor`）：仅声明表单子集（`projectName`(text，创建后不可改) / `displayName`(text)）；其余由 Form↔YAML 双模承载（U4）。

**credential**（动态，见 §6）：`providerId`(select) + 供应商字段。

## 6. 动态 schema：DNS 凭证

`dns-provider` 能力（`extension.DNSProviderSpec.Fields`，`ProviderField`）泛化为 `ConfigField`：

| ProviderField | ConfigField |
|---|---|
| `Name` | `Key` |
| `Label` | `Label` |
| `Type: text\|password` | `ConfigText` / `ConfigPassword` |
| `Required` | `Required` |
| `Secret` | （`ConfigPassword` 已隐含） |

`GET /api/v1/schema/credential?provider=<id>`：先返回 `providerId` 字段（选项来自扩展注册表 `DNSProviders()`），再追加该 provider 的字段。**兼容**：`GET /api/v1/credentials/providers` 保留不动（现有凭证表单继续可用），`/schema/credential` 是其泛化入口。

## 7. 自定义渲染逃生舱（`uiHint`）

复杂事实（gateway Service、compose）的交互远超纯字段 schema（域名行、上游、片段开关、双模编辑器）。`uiHint` 让前端选择**域定制组件**，而非强行用通用表单：

- 前端：`uiHint` 命中 `modules/gateway/components/...` 等定制渲染器；未命中则用 U4 的通用 `schema-form`。
- 后端 schema 仍提供**字段元数据**（供通用表单兜底、校验、YAML 补全复用）。
- `uiHint` 是**提示**不是契约：前端未知时安全回退到通用表单。

## 8. API 契约

`GET /api/v1/schema/{factKind}[?provider=<id>]` →

```json
{
  "factKind": "service",
  "uiHint": "gateway-service",
  "fields": [
    { "key": "appId", "label": "归属应用", "type": "reference", "required": true,
      "reference": { "types": ["app"] } },
    { "key": "domains", "label": "发布域名", "type": "array",
      "item": { "type": "object", "fields": [ /* ... */ ] } }
  ]
}
```

- 未知 `factKind` → `404`，`{"error":{"code":"UNKNOWN_FACT_KIND","message":"未知事实类别"}}`。
- 新增 `handler.RegisterSchema(mux, schemaProvider)`，`server.New` 装配；schema 来源 = 事实注册表 + 扩展注册表（credential）。
- 字段顺序即数组顺序（不入库，稳定）。

## 9. 兼容与迁移

- 不删除任何现有端点；`/credentials/providers` 与 `CredentialFormModal` 继续工作。
- `ProviderField` 保留（`extension` 包对外），新增 `ParseDNSProvider` 到 `ConfigField` 的转换函数，避免破坏扩展契约。
- 前端在 U4 才消费 `/schema`；U3 只需端点可用 + 测试。

## 10. 测试与验收

- `schema_test.go`：
  - 每个持久事实类别都有非空 `Schema`（`service/domain/port/variable/fragment/compose` 逐项断言）。
  - `credential` schema 随 `provider` 变化（cloudflare 单 token；dnspod 双字段）。
  - `ProviderField → ConfigField` 映射（password/required）。
  - 未知 factKind 返回 404。
- 契约测试：`/schema/service` 返回 camelCase、`uiHint` 稳定。
- 门禁：`go build ./...` / `go vet ./...` / `gofmt -l` / `mise run test`。

## 11. 开放问题（待评审）

1. **`service` 字段随 `type` 联动**（reverse_proxy 需要 upstream，file_server 需要 root）：L1 是否只声明全集 + `advanced`，联动交给前端？倾向：**L1 全集 + 前端简单联动**，条件表达式后置。
2. **schema 是否需要 i18n**：字段 label 目前中文硬编码（与现状一致）；是否预留 `labelKey`？倾向：**L1 中文硬编码**，i18n 后置。
3. **`compose` 的 schema 粒度**：表单只声明少量字段、其余靠双模，是否可接受？倾向：**可接受**，compose 的真相是 YAML。
4. **`variable.secret`**：新增字段还是复用密码类型？倾向：**用 `ConfigPassword` 类型**，不加 `secret` 开关。
