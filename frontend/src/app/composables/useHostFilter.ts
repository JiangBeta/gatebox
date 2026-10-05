import { computed, ref } from 'vue'
import { listObjects, type V41Object } from '@/api/objects'

/**
 * 主机维度（多主机 · ADR-042 §13）。
 *
 * 原则：主机是「维度」不是「页面」——顶部主机条（筛选）+ 每表主机列 + 离线降级。
 * 数据源是 host 对象（后端启动时自动建好本机 edge，见 backend objects/host.go）。
 *
 * 离线降级是硬要求：离线主机的资源状态标「未知」、操作按钮禁用。
 * 不能假装还在——离线主机上的「停止」按钮点了只会静默失败，
 * 用户会以为容器真的停了。
 *
 * 筛选状态是模块级单例：容器/编排/镜像…各 Tab 都在同一屏下方并排，
 * 共享一个筛选才对（原型 hostFilter 是全局变量）。
 */

const hosts = ref<V41Object[]>([])
/** 当前筛选的主机名；'all' = 全部主机。 */
const filter = ref('all')
/** 每主机的资源数，由各 Tab 用自己的列表回填（counts[host]）。 */
const counts = ref<Record<string, number>>({})
let inflight: Promise<void> | null = null

async function reload(): Promise<void> {
  inflight ??= (async () => {
    try {
      hosts.value = await listObjects('host')
    } catch {
      /* 主机列表拿不到：页面按「本机」继续，不阻塞其它内容 */
    } finally {
      inflight = null
    }
  })()
  return inflight
}
void reload()

/** 主机名（host 对象的主键字段是 host，不是 id）。 */
function hostName(h: V41Object): string {
  return String(h.spec?.host ?? h.key)
}

const online = computed(() => hosts.value.filter((h) => h.status?.online === true))
const offline = computed(() => hosts.value.filter((h) => h.status?.online !== true))

function findHost(name?: string | null): V41Object | undefined {
  if (!name) return undefined
  return hosts.value.find((h) => hostName(h) === name)
}

/**
 * isOnline 某主机是否在线。
 *
 * host 名查不到时返回 true（视为本机 / 对象还没建）：docker 资源本来就来自
 * 本机 daemon，这时判成「离线」会让整页操作按钮全灰——比信息缺失更糟。
 * 真离线的判据是对象 status.online（后端探测得出）。
 */
function isOnline(name?: string | null): boolean {
  const h = findHost(name)
  if (!h) return true
  return h.status?.online === true
}

function addressOf(name?: string | null): string {
  return findHost(name) ? String(findHost(name)?.spec?.address ?? '') : ''
}

function lastSeenOf(name?: string | null): string {
  const v = findHost(name)?.status?.lastSeen
  return v ? String(v) : ''
}

/**
 * match 资源是否落在当前筛选的主机里。
 *
 * 资源没有 host 字段（本机直连 / 旧数据）时一律通过：按主机筛选
 * 不该把「没标主机」的东西从列表里抹掉——那看起来像数据丢了。
 */
function match(name?: string | null): boolean {
  if (filter.value === 'all') return true
  if (!name) return true
  return name === filter.value
}

function setFilter(v: string) {
  filter.value = v
}

function setCounts(c: Record<string, number>) {
  counts.value = c
}

/**
 * countByHost 按 host 字段统计资源数。
 *
 * 各 Tab 把自己拉到的列表喂进来，主机条上的角标就跟着这张表走
 * （角标语义 = 「该主机下当前能看到的这类资源有多少」）。
 * 没有 host 字段的条目不计数：那是本机直连/旧数据，不该摊到任何主机头上。
 */
export function countByHost<T extends { host?: string }>(list: T[]): Record<string, number> {
  const out: Record<string, number> = {}
  for (const item of list) {
    if (!item.host) continue
    out[item.host] = (out[item.host] ?? 0) + 1
  }
  return out
}

/** countOf 某主机的资源数；没回填时返回 undefined（UI 显示为空而不是 0）。 */
function countOf(name: string): number | undefined {
  return counts.value[name]
}

/**
 * localName 本机 daemon 对应的主机名。
 *
 * Docker API 目前只连本机 daemon（ADR-014 的轻量 HTTP 封装），
 * 所以「清理未使用卷」这类 **daemon 级** 操作只能落在这台主机上。
 * 筛选到别的主机时必须禁用按钮并说明原因——否则用户会以为
 * 远端主机被清理了，实际删的是本机的卷。
 * 判据用 host 对象的 role=edge（后端启动时建好，见 objects/host.go）。
 */
const localName = computed(() => {
  const edge = hosts.value.find((h) => String(h.spec?.role ?? '') === 'edge')
  return edge ? hostName(edge) : 'edge'
})

/** isLocal 主机名是否就是本机 daemon。 */
function isLocal(name?: string | null): boolean {
  return name === localName.value
}

/** tabHosts 可直接用于渲染的主机清单（含名字、地址、在线态）。 */
const tabHosts = computed(() =>
  hosts.value.map((h) => ({
    name: hostName(h),
    address: String(h.spec?.address ?? ''),
    online: h.status?.online === true,
    lastSeen: h.status?.lastSeen ? String(h.status.lastSeen) : '',
  })),
)

export function useHostFilter() {
  return {
    hosts,
    tabHosts,
    online,
    offline,
    filter,
    isOnline,
    addressOf,
    lastSeenOf,
    match,
    setFilter,
    setCounts,
    countOf,
    localName,
    isLocal,
    reload,
  }
}