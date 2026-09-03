import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Search, Puzzle, Check } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { EmptyRail } from '@/components/common/Rail'
import { ErrorPanel } from '@/components/common/EmptyState'
import { Section } from '@/components/common/Page'
import { PAGE_SIZES, Pagination } from '@/components/common/Pagination'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import { cn } from '@/lib/utils'
import {
  COMPONENT_CATEGORIES,
  COMPONENT_SHAPE_META,
  UNCATEGORIZED,
  categoryLabel,
  componentConfig,
  componentDescription,
  componentShape,
} from '@/lib/components/taxonomy'
import type { components } from '@/lib/api/schema'

type Resource = components['schemas']['Resource']
type Plugin = components['schemas']['Plugin']

type Tab = 'plugin' | 'custom'
type StatusFilter = 'all' | 'enabled' | 'disabled'

/**
 * 组件广场——卡片墙 + 两行筛选 + 分页，取代原来那个和其它资源类型共用的
 * 单行列表。组件是五种资源里形态最多的一种（HTTP 接口 / OpenAPI 导入的一
 * 批 operation / 沙箱环境，以后还有插件），一行一个 ref 的列表既看不出这
 * 条是什么，也没有地方摆使用场景——卡片墙是为了让人"挑"，列表只适合让人
 * "核对"。
 *
 * 顶部 插件/自定义 两个 Tab：自定义是这个账号自己注册的组件；插件是平台统
 * 一提供、开箱即用的一档（spec-20），走 GET /plugins/market 拿到的公开市场
 * 列表——只有 visibility=public 且 review_status=passed 的版本才会出现在
 * 这里，安装前会先展示 manifest.requires.permissions 让用户知道自己在
 * 授权什么。
 */
export function ComponentPlazaPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [searchParams, setSearchParams] = useSearchParams()
  const tab: Tab = searchParams.get('tab') === 'custom' ? 'custom' : 'plugin'
  function setTab(t: Tab) {
    setSearchParams(t === 'plugin' ? {} : { tab: t })
  }
  const [draft, setDraft] = useState('')
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [category, setCategory] = useState('all')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(PAGE_SIZES[0])
  const [toggleError, setToggleError] = useState<string | null>(null)

  const query = useQuery({
    queryKey: ['resources', 'tool'],
    queryFn: async () =>
      unwrap<{ items: Resource[]; has_more: boolean }>(
        await apiClient.GET('/resources', { params: { query: { type: 'tool' } } }),
      ),
  })

  const pluginQuery = useQuery({
    queryKey: ['plugins', 'market'],
    queryFn: async () => unwrap<{ items: Plugin[] }>(await apiClient.GET('/plugins/market', {})),
    enabled: tab === 'plugin',
  })
  const pluginItems = useMemo(() => pluginQuery.data?.items ?? [], [pluginQuery.data])

  const installedQuery = useQuery({
    queryKey: ['plugins', 'installed'],
    queryFn: async () =>
      unwrap<{ items: components['schemas']['PluginInstallation'][] }>(await apiClient.GET('/plugins/installed', {})),
    enabled: tab === 'plugin',
  })
  const installedPluginIDs = useMemo(
    () => new Set((installedQuery.data?.items ?? []).map((i) => i.plugin_id)),
    [installedQuery.data],
  )

  const [installTarget, setInstallTarget] = useState<Plugin | null>(null)

  const items = useMemo(() => query.data?.items ?? [], [query.data])

  const filtered = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    return items.filter((r) => {
      const config = componentConfig(r.config)
      if (status === 'enabled' && r.status !== 1) return false
      if (status === 'disabled' && r.status === 1) return false
      if (category !== 'all' && (config.category ?? '') !== category) return false
      if (!keyword) return true
      return (
        r.ref.toLowerCase().includes(keyword) ||
        (r.display_name ?? '').toLowerCase().includes(keyword) ||
        componentDescription(config).toLowerCase().includes(keyword)
      )
    })
  }, [items, search, status, category])

  // 换筛选条件/每页条数后停在第 5 页会看到一片空白——任何一个会改变结果
  // 集长度的操作都把页码打回第一页。
  useEffect(() => {
    setPage(1)
  }, [search, status, category, pageSize, tab])

  const pageCount = Math.max(1, Math.ceil(filtered.length / pageSize))
  const visible = filtered.slice((page - 1) * pageSize, page * pageSize)

  async function toggleStatus(r: Resource) {
    setToggleError(null)
    const nextStatus = r.status === 1 ? 2 : 1
    try {
      unwrap(
        await apiClient.PATCH('/resources/{id}', {
          params: { path: { id: r.id } },
          body: { status: nextStatus },
        }),
      )
      queryClient.invalidateQueries({ queryKey: ['resources', 'tool'] })
    } catch (err) {
      setToggleError(err instanceof ApiError ? err.message : '操作没能完成，请再试一次')
    }
  }

  function commitSearch() {
    setSearch(draft)
  }

  return (
    <Section
      title="组件广场"
      center={
        <div
          role="tablist"
          className="flex w-fit items-center gap-space-1 rounded-sm border border-border bg-surface-muted p-1"
        >
          {(['plugin', 'custom'] as const).map((t) => (
            <button
              key={t}
              type="button"
              role="tab"
              aria-selected={tab === t}
              onClick={() => setTab(t)}
              className={cn(
                'text-body-sm rounded-sm px-space-4 py-1.5 transition-colors',
                tab === t ? 'bg-surface text-ink-900 shadow-sm' : 'text-ink-500 hover:text-ink-900',
              )}
            >
              {t === 'plugin' ? '广场' : '自定义'}
            </button>
          ))}
        </div>
      }
      aside={
        <Button className="bg-gradient-cta text-white hover:opacity-90" onClick={() => navigate('/apps/tool/new')}>
          新建组件
        </Button>
      }
    >

      {tab === 'plugin' ? (
        <>
          {pluginQuery.isLoading && <CardGridSkeleton />}

          {pluginQuery.isError && (
            <ErrorPanel message="插件市场没能加载出来" onRetry={() => pluginQuery.refetch()} />
          )}

          {pluginQuery.isSuccess && pluginItems.length === 0 && (
            <EmptyRail
              title="市场上还没有已上架的插件"
              description="插件由发布者上传并申请上架，经管理员审核通过后才会出现在这里——和这里自己注册的组件走同一套引用方式（Agent 的能力白名单里一个 ref 一个工具），只是不需要你自己填地址和凭证。"
            />
          )}

          {pluginItems.length > 0 && (
            <ul className="grid grid-cols-1 gap-space-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5">
              {pluginItems.map((p) => (
                <PluginCard
                  key={p.id}
                  plugin={p}
                  installed={installedPluginIDs.has(p.plugin_id)}
                  onInstall={() => setInstallTarget(p)}
                />
              ))}
            </ul>
          )}

          <PluginInstallDialog
            plugin={installTarget}
            onClose={() => setInstallTarget(null)}
            onInstalled={() => {
              toast('安装成功')
              queryClient.invalidateQueries({ queryKey: ['plugins'] })
            }}
          />
        </>
      ) : (
        <>
          <div className="flex flex-wrap items-center justify-end gap-space-2">
            <div className="relative">
              <Search
                aria-hidden
                className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-ink-500"
              />
              <Input
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && commitSearch()}
                placeholder="搜索组件名称"
                aria-label="搜索组件名称"
                className="h-9 w-64 pl-9"
              />
            </div>
            <Button variant="outline" className="h-9" onClick={commitSearch}>
              搜索
            </Button>
          </div>

          <div className="flex flex-col gap-space-2">
            <FilterRow
              label="状态"
              value={status}
              onChange={(v) => setStatus(v as StatusFilter)}
              options={[
                { value: 'all', label: '全部类型' },
                { value: 'enabled', label: '已启用' },
                { value: 'disabled', label: '已停用' },
              ]}
            />
            <FilterRow
              label="使用场景"
              value={category}
              onChange={setCategory}
              options={[
                { value: 'all', label: '全部' },
                ...COMPONENT_CATEGORIES.map((c) => ({ value: c.value, label: c.label })),
                { value: '', label: UNCATEGORIZED },
              ]}
            />
          </div>

          {toggleError && (
            <p role="alert" className="text-body-sm text-rust">
              {toggleError}
            </p>
          )}

          {query.isLoading && <CardGridSkeleton />}

          {query.isError && <ErrorPanel message="组件列表没能加载出来" onRetry={() => query.refetch()} />}

          {query.isSuccess && items.length === 0 && (
            <EmptyRail
              title="给 Agent 一件能用的工具"
              description="组件是 Agent 能调用的外部能力：一个检索接口、一批从 OpenAPI 导入的操作、一个能跑代码的沙箱环境……注册后才能写进 Agent 的能力白名单。"
              action={
                <Button
                  size="sm"
                  className="bg-gradient-cta text-white hover:opacity-90"
                  onClick={() => navigate('/apps/tool/new')}
                >
                  新建组件
                </Button>
              }
            />
          )}

          {query.isSuccess && items.length > 0 && filtered.length === 0 && (
            <EmptyRail
              title="没有符合当前筛选条件的组件"
              description="搜索只匹配组件的 ref、名称和说明，不搜索配置里的地址与凭证。"
              action={
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setDraft('')
                    setSearch('')
                    setStatus('all')
                    setCategory('all')
                  }}
                >
                  清除筛选
                </Button>
              }
            />
          )}

          {visible.length > 0 && (
            <ul className="grid grid-cols-1 gap-space-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5">
              {visible.map((r) => (
                <ComponentCard
                  key={r.id}
                  resource={r}
                  onToggle={() => toggleStatus(r)}
                  onOpen={() => navigate(`/apps/tool/${r.id}`)}
                />
              ))}
            </ul>
          )}

          {filtered.length > 0 && (
            <Pagination
              page={page}
              pageCount={pageCount}
              pageSize={pageSize}
              onPageChange={setPage}
              onPageSizeChange={setPageSize}
            />
          )}
        </>
      )}
    </Section>
  )
}

function FilterRow({
  label,
  value,
  onChange,
  options,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  options: { value: string; label: string }[]
}) {
  return (
    <div className="flex items-center gap-space-2">
      <span className="text-body-sm w-16 shrink-0 text-ink-500">{label}</span>
      <div className="flex flex-wrap items-center gap-space-1">
        {options.map((opt) => (
          <button
            key={opt.value || 'uncategorized'}
            type="button"
            aria-pressed={value === opt.value}
            onClick={() => onChange(opt.value)}
            className={cn(
              'text-body-sm rounded-md px-space-3 py-1 transition-colors',
              value === opt.value ? 'bg-blueprint-tint text-blueprint' : 'text-ink-700 hover:bg-surface-muted',
            )}
          >
            {opt.label}
          </button>
        ))}
      </div>
    </div>
  )
}

function ComponentCard({
  resource,
  onToggle,
  onOpen,
}: {
  resource: Resource
  onToggle: () => void
  onOpen: () => void
}) {
  const config = componentConfig(resource.config)
  const shape = COMPONENT_SHAPE_META[componentShape(config)]
  const Icon = shape.icon
  const enabled = resource.status === 1
  const name = resource.display_name || resource.ref

  return (
    <li className="flex flex-col gap-space-3 rounded-lg border border-border bg-surface p-space-4">
      <div className="flex items-start gap-space-3">
        <span
          aria-hidden
          className={cn(
            'grid size-8 shrink-0 place-items-center rounded-md',
            enabled ? 'bg-blueprint-tint text-blueprint' : 'bg-surface-muted text-ink-500',
          )}
        >
          <Icon className="size-4" />
        </span>
        <span className="flex min-w-0 flex-1 flex-col items-start">
          {/* 标题即入口。整张卡不做点击区——卡上还有"停用"，套一层点击区容
              易误触。 */}
          <button
            type="button"
            onClick={onOpen}
            className="text-label-md max-w-full truncate text-ink-900 hover:text-blueprint hover:underline"
            title={name}
          >
            {name}
          </button>
          <span className="text-ref max-w-full truncate text-ink-500" title={resource.ref}>
            {resource.ref}
          </span>
        </span>
        <Button
          variant="ghost"
          size="sm"
          className="text-caption -mt-1 -mr-2 h-7 shrink-0 px-space-2 text-ink-500"
          onClick={onToggle}
        >
          {enabled ? '停用' : '启用'}
        </Button>
      </div>

      <p className="text-body-sm line-clamp-2 min-h-10 text-ink-500">{componentDescription(config)}</p>

      <div className="flex items-center justify-between gap-space-2">
        <span className="text-caption shrink-0 rounded-sm bg-surface-muted px-space-2 py-0.5 text-ink-700">
          {categoryLabel(config.category)}
        </span>
        <span className="flex shrink-0 items-center gap-space-2">
          <span className={cn('text-caption truncate', enabled ? 'text-ink-500' : 'text-rust')}>
            {enabled ? shape.label : '已停用'}
          </span>
          <Button variant="ghost" size="sm" className="text-caption h-7 px-space-2 text-blueprint" onClick={onOpen}>
            详情
          </Button>
        </span>
      </div>
    </li>
  )
}

type PluginManifest = {
  description?: string
  requires?: { permissions?: string[] }
  extensions?: { connector_hint?: { dialect: string; default_port?: number } }
}

function pluginManifest(p: Plugin): PluginManifest {
  return (p.manifest ?? {}) as PluginManifest
}

function PluginCard({ plugin, installed, onInstall }: { plugin: Plugin; installed: boolean; onInstall: () => void }) {
  const manifest = pluginManifest(plugin)
  const name = plugin.display_name || plugin.plugin_id

  return (
    <li className="flex flex-col gap-space-3 rounded-lg border border-border bg-surface p-space-4">
      <div className="flex items-start gap-space-3">
        <span aria-hidden className="grid size-8 shrink-0 place-items-center rounded-md bg-blueprint-tint text-blueprint">
          <Puzzle className="size-4" />
        </span>
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="text-label-md truncate text-ink-900" title={name}>
            {name}
          </span>
          <span className="text-ref truncate text-ink-500" title={plugin.plugin_id}>
            {plugin.plugin_id}@{plugin.version}
          </span>
        </span>
        {installed ? (
          <span className="text-caption -mt-1 -mr-2 flex h-7 shrink-0 items-center gap-1 px-space-2 text-emerald-600">
            <Check className="size-3.5" aria-hidden />
            已安装
          </span>
        ) : (
          <Button variant="ghost" size="sm" className="text-caption -mt-1 -mr-2 h-7 shrink-0 px-space-2 text-ink-500" onClick={onInstall}>
            安装
          </Button>
        )}
      </div>

      <p className="text-body-sm line-clamp-2 min-h-10 text-ink-500">{manifest.description || '（发布者未填写说明）'}</p>
    </li>
  )
}

/**
 * 安装前把 manifest.requires.permissions 摊开来给用户看——插件运行在
 * WASM 沙箱里，能碰到什么完全由这份权限声明决定，装之前应该让用户知道
 * 自己在同意什么，而不是装完才发现。
 */
const EMPTY_CONNECTOR_FORM = { host: '', port: '', database: '', username: '', password: '', allowWrite: false }

/**
 * sanitizeRefPrefix turns a plugin_id (dots allowed, e.g.
 * "agentic-kit.postgres-connector") into something that satisfies the
 * resource center's ref pattern (^[a-z][a-z0-9_-]*$, no dots) — a random
 * suffix is appended so installing the same connector plugin twice (two
 * different databases) never collides on ref uniqueness.
 */
function connectorResourceRef(pluginID: string): string {
  const base = pluginID.toLowerCase().replace(/[^a-z0-9-]+/g, '-')
  return `${base}-${Math.random().toString(36).slice(2, 8)}`
}

/**
 * 安装前把 manifest.requires.permissions 摊开来给用户看——插件运行在
 * WASM 沙箱里，能碰到什么完全由这份权限声明决定，装之前应该让用户知道
 * 自己在同意什么，而不是装完才发现。
 *
 * 数据库连接器插件（manifest.extensions.connector_hint 声明了 dialect）
 * 额外需要真实的连接信息——sql.query/execute/schema 这些 host function
 * 靠的是宿主手里的一个真实连接，不是插件自己拼连接串（spec-20 §4.3）。
 * 这里的做法是先把这些字段包成一个"组件"资源（component_type=connector，
 * 和沙箱、HTTP 接口走同一张 tools 表，只是多几个字段），拿到它的资源 id，
 * 再把这个 id 写进安装配置的 connector_resource_id——和运行时
 * bindConnector 解析这个字段的方式（internal/adapter/orchestrator/
 * plugin_authorizer.go）必须是同一套资源 id 编码，所以这里直接用
 * POST /resources 返回的 id 原样传回去，不自己拼数字。
 */
function PluginInstallDialog({
  plugin,
  onClose,
  onInstalled,
}: {
  plugin: Plugin | null
  onClose: () => void
  onInstalled: () => void
}) {
  const [installing, setInstalling] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [connectorForm, setConnectorForm] = useState(EMPTY_CONNECTOR_FORM)

  useEffect(() => {
    setError(null)
    setInstalling(false)
    const hint = plugin ? pluginManifest(plugin).extensions?.connector_hint : undefined
    setConnectorForm({ ...EMPTY_CONNECTOR_FORM, port: hint?.default_port ? String(hint.default_port) : '' })
  }, [plugin])

  if (!plugin) return null
  const manifest = pluginManifest(plugin)
  const permissions = manifest.requires?.permissions ?? []
  const connectorHint = manifest.extensions?.connector_hint

  const connectorFieldsFilled =
    connectorForm.host.trim() !== '' &&
    connectorForm.port.trim() !== '' &&
    connectorForm.database.trim() !== '' &&
    connectorForm.username.trim() !== '' &&
    connectorForm.password.trim() !== ''
  const canInstall = !connectorHint || connectorFieldsFilled

  function setConnectorField<K extends keyof typeof EMPTY_CONNECTOR_FORM>(key: K, value: (typeof EMPTY_CONNECTOR_FORM)[K]) {
    setConnectorForm((prev) => ({ ...prev, [key]: value }))
  }

  async function install() {
    if (!plugin) return
    setInstalling(true)
    setError(null)
    try {
      let installConfig: Record<string, unknown> | undefined
      if (connectorHint) {
        const resource = unwrap<{ id: string }>(
          await apiClient.POST('/resources', {
            body: {
              type: 'tool',
              ref: connectorResourceRef(plugin.plugin_id),
              display_name: `${plugin.display_name || plugin.plugin_id} 连接`,
              config: {
                component_type: 'connector',
                dialect: connectorHint.dialect,
                host: connectorForm.host.trim(),
                port: Number(connectorForm.port),
                database: connectorForm.database.trim(),
                username: connectorForm.username.trim(),
                password: connectorForm.password,
                allow_write: connectorForm.allowWrite,
              },
            },
          }),
        )
        installConfig = { connector_resource_id: resource.id }
      }

      unwrap(
        await apiClient.POST('/plugins/{id}/install', {
          params: { path: { id: plugin.plugin_id } },
          body: {
            granted: permissions,
            ...(installConfig ? { config: installConfig as Record<string, never> } : {}),
          },
        }),
      )
      onInstalled()
      onClose()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '安装没能完成，请再试一次')
      setInstalling(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>安装「{plugin.display_name || plugin.plugin_id}」</DialogTitle>
          <DialogDescription>
            {plugin.plugin_id}@{plugin.version}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-space-4">
          <div className="flex flex-col gap-space-2">
            <p className="text-body-sm text-ink-700">
              {permissions.length > 0 ? '安装后这个插件将拥有以下权限：' : '这个插件没有声明任何额外权限。'}
            </p>
            {permissions.length > 0 && (
              <ul className="flex flex-col gap-space-1 rounded-md bg-surface-muted p-space-3">
                {permissions.map((perm) => (
                  <li key={perm} className="text-body-sm font-mono text-ink-900">
                    {perm}
                  </li>
                ))}
              </ul>
            )}
          </div>

          {connectorHint && (
            <div className="flex flex-col gap-space-3">
              <p className="text-body-sm text-ink-700">
                这是一个 {connectorHint.dialect} 连接器，需要真实的数据库连接信息——宿主会用它建立连接，插件本身永远拿不到。
              </p>
              <div className="grid grid-cols-2 gap-space-3">
                <ConnectorField label="主机地址" value={connectorForm.host} onChange={(v) => setConnectorField('host', v)} placeholder="db.internal" />
                <ConnectorField label="端口" value={connectorForm.port} onChange={(v) => setConnectorField('port', v)} placeholder={String(connectorHint.default_port ?? '')} />
                <ConnectorField label="数据库名" value={connectorForm.database} onChange={(v) => setConnectorField('database', v)} />
                <ConnectorField label="用户名" value={connectorForm.username} onChange={(v) => setConnectorField('username', v)} />
                <ConnectorField label="密码" value={connectorForm.password} onChange={(v) => setConnectorField('password', v)} type="password" />
              </div>
              <label className="flex items-center gap-space-2 text-body-sm text-ink-900">
                <Checkbox checked={connectorForm.allowWrite} onCheckedChange={(checked) => setConnectorField('allowWrite', checked === true)} />
                允许写入（UPDATE/INSERT/DELETE）——不勾选时这个连接只能读
              </label>
            </div>
          )}

          {error && (
            <p role="alert" className="text-body-sm text-rust">
              {error}
            </p>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={installing}>
            取消
          </Button>
          <Button className="bg-gradient-cta text-white hover:opacity-90" onClick={install} disabled={installing || !canInstall}>
            {installing ? '安装中…' : '确认安装'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ConnectorField({
  label,
  value,
  onChange,
  placeholder,
  type = 'text',
}: {
  label: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  type?: string
}) {
  return (
    <label className="flex flex-col gap-space-1">
      <span className="text-caption text-ink-500">{label}</span>
      <Input type={type} value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} className="h-9" />
    </label>
  )
}

function CardGridSkeleton() {
  return (
    <div
      aria-hidden
      className="grid grid-cols-1 gap-space-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5"
    >
      {Array.from({ length: 8 }).map((_, i) => (
        <div key={i} className="flex flex-col gap-space-3 rounded-lg border border-border bg-surface p-space-4">
          <div className="flex items-center gap-space-3">
            <div className="size-8 shrink-0 animate-pulse rounded-md bg-surface-muted" />
            <div className="h-3 w-1/2 animate-pulse rounded-xs bg-surface-muted" />
          </div>
          <div className="h-3 w-full animate-pulse rounded-xs bg-surface-muted" />
          <div className="h-3 w-2/3 animate-pulse rounded-xs bg-surface-muted" />
        </div>
      ))}
    </div>
  )
}
