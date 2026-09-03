import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import { cn } from '@/lib/utils'
import {
  COMPONENT_CATEGORIES,
  COMPONENT_SHAPE_META,
  UNCATEGORIZED,
  componentConfig,
  componentShape,
} from '@/lib/components/taxonomy'
import type { components } from '@/lib/api/schema'

type Resource = components['schemas']['Resource']

/** config 里由本页表单接管的字段；其余字段原样带回，不因为编辑而丢失。 */
const EDITABLE_KEYS = ['description', 'category', 'endpoint', 'method', 'path', 'base_url'] as const

interface FormState {
  displayName: string
  description: string
  category: string
  endpoint: string
  method: string
  path: string
  baseURL: string
}

function formOf(r: Resource): FormState {
  const c = componentConfig(r.config)
  return {
    displayName: r.display_name ?? '',
    description: c.description ?? '',
    category: c.category ?? '',
    endpoint: c.endpoint ?? '',
    method: c.method ?? '',
    path: c.path ?? '',
    baseURL: (r.config as Record<string, unknown>)?.base_url as string ?? '',
  }
}

/**
 * 组件详情 / 编辑。
 *
 * 保存走 PATCH /resources/{id}，config 用的是"凭证保留"的合并语义：这个页
 * 面拿到的 config 是 Redact 过的（凭证字段根本不在里面），原样交回去不会把
 * 已存的密钥清空——所以表单不需要、也不该把密钥回填出来给人看。
 *
 * 表单只接管少数几个字段，config 里其它键（component_type / tool_type / 
 * operation 参数等运行时真正要读的东西）原样带回。编辑显示名不该顺手把
 * Agent 调用这个组件的方式改掉。
 */
export function ComponentDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [form, setForm] = useState<FormState | null>(null)

  const query = useQuery({
    queryKey: ['resource', id],
    queryFn: async () =>
      unwrap<Resource>(await apiClient.GET('/resources/{id}', { params: { path: { id: id! } } })),
    enabled: Boolean(id),
  })

  const resource = query.data
  // 读回来之后灌一次表单。用 resource 本身做依赖，保存成功后拿到新数据会
  // 重新对齐，不会停在旧值上。
  useEffect(() => {
    if (resource) setForm(formOf(resource))
  }, [resource])

  const shape = resource ? componentShape(componentConfig(resource.config)) : 'http'
  const shapeMeta = COMPONENT_SHAPE_META[shape]

  // config 里表单没接管的那些键，保存时原样带回去。
  const passthroughConfig = useMemo(() => {
    const c = (resource?.config ?? {}) as Record<string, unknown>
    const out: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(c)) {
      if (!(EDITABLE_KEYS as readonly string[]).includes(k)) out[k] = v
    }
    return out
  }, [resource])

  const dirty = useMemo(() => {
    if (!resource || !form) return false
    return JSON.stringify(form) !== JSON.stringify(formOf(resource))
  }, [resource, form])

  const saveMutation = useMutation({
    mutationFn: async (f: FormState) => {
      // 空串的可选字段就不要塞进 config 了，否则 config 里会攒下一堆
      // "": "" 的噪音，广场页的兜底描述也会因此失效。
      const config: Record<string, unknown> = { ...passthroughConfig }
      const put = (k: string, v: string) => {
        if (v.trim()) config[k] = v.trim()
      }
      put('description', f.description)
      put('category', f.category)
      put('endpoint', f.endpoint)
      put('method', f.method)
      put('path', f.path)
      put('base_url', f.baseURL)

      return unwrap<Resource>(
        await apiClient.PATCH('/resources/{id}', {
          params: { path: { id: id! } },
          body: { display_name: f.displayName.trim(), config },
        }),
      )
    },
    onSuccess: (updated) => {
      toast.success('已保存')
      queryClient.setQueryData(['resource', id], updated)
      queryClient.invalidateQueries({ queryKey: ['resources', 'tool'] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : '保存没能完成，请再试一次'),
  })

  const toggleMutation = useMutation({
    mutationFn: async () =>
      unwrap<Resource>(
        await apiClient.PATCH('/resources/{id}', {
          params: { path: { id: id! } },
          body: { status: resource?.status === 1 ? 2 : 1 },
        }),
      ),
    onSuccess: (updated) => {
      toast.success(updated.status === 1 ? '已启用' : '已停用')
      queryClient.setQueryData(['resource', id], updated)
      queryClient.invalidateQueries({ queryKey: ['resources', 'tool'] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : '操作没能完成，请再试一次'),
  })

  if (query.isLoading) return <ListSkeleton />
  if (query.isError || !resource || !form) {
    return <ErrorPanel message="这个组件没能加载出来" onRetry={() => query.refetch()} />
  }

  const enabled = resource.status === 1
  const set = (patch: Partial<FormState>) => setForm({ ...form, ...patch })

  return (
    <div className="flex flex-col gap-space-6">
      <div className="flex flex-wrap items-center gap-space-3">
        <Button variant="ghost" size="sm" onClick={() => navigate('/apps/tool?tab=custom')}>
          <ArrowLeft className="size-4" aria-hidden />
          返回
        </Button>
        <div className="flex min-w-0 flex-col">
          <span className="text-label-md truncate text-ink-900">{resource.display_name || resource.ref}</span>
          <span className="text-caption flex flex-wrap items-center gap-space-2 text-ink-500">
            <span className="text-ref">{resource.ref}</span>
            <span className="rounded-full bg-surface-muted px-space-2 py-0.5">{shapeMeta.label}</span>
            {!enabled && <span className="text-rust">已停用</span>}
          </span>
        </div>
        <Button
          variant="outline"
          size="sm"
          className="ml-auto"
          disabled={toggleMutation.isPending}
          onClick={() => toggleMutation.mutate()}
        >
          {enabled ? '停用' : '启用'}
        </Button>
      </div>

      <div className="flex flex-col gap-space-4 rounded-lg border border-border bg-surface p-space-5">
        <label className="flex flex-col gap-space-2">
          <span className="text-label-md text-ink-900">显示名</span>
          <Input value={form.displayName} onChange={(e) => set({ displayName: e.target.value })} />
        </label>

        <label className="flex flex-col gap-space-2">
          <span className="text-label-md text-ink-900">说明</span>
          <Textarea rows={2} value={form.description} onChange={(e) => set({ description: e.target.value })} />
          <span className="text-caption text-ink-500">留空时广场卡片会拿接口地址凑一句。</span>
        </label>

        <div className="flex flex-col gap-space-2">
          <span className="text-label-md text-ink-900">分类</span>
          <div className="flex flex-wrap gap-space-2">
            {[...COMPONENT_CATEGORIES, { value: '', label: UNCATEGORIZED }].map((c) => (
              <button
                key={c.value || 'none'}
                type="button"
                onClick={() => set({ category: c.value })}
                className={cn(
                  'text-body-sm rounded-full border px-space-3 py-1 transition-colors',
                  form.category === c.value
                    ? 'border-blueprint bg-blueprint-tint text-blueprint'
                    : 'border-border text-ink-700 hover:border-border-strong',
                )}
              >
                {c.label}
              </button>
            ))}
          </div>
        </div>

        {/* 形态不同，能改的接口字段不同：手填的 HTTP 组件改 endpoint，
            OpenAPI 导入的那批改 base_url + method/path。沙箱两者都没有。 */}
        {shape === 'http' && (
          <label className="flex flex-col gap-space-2">
            <span className="text-label-md text-ink-900">接口地址</span>
            <Input value={form.endpoint} onChange={(e) => set({ endpoint: e.target.value })} />
          </label>
        )}

        {shape === 'openapi' && (
          <>
            <label className="flex flex-col gap-space-2">
              <span className="text-label-md text-ink-900">服务地址</span>
              <Input value={form.baseURL} onChange={(e) => set({ baseURL: e.target.value })} />
            </label>
            <div className="flex gap-space-3">
              <label className="flex w-32 flex-col gap-space-2">
                <span className="text-label-md text-ink-900">方法</span>
                <Input value={form.method} onChange={(e) => set({ method: e.target.value })} />
              </label>
              <label className="flex flex-1 flex-col gap-space-2">
                <span className="text-label-md text-ink-900">路径</span>
                <Input value={form.path} onChange={(e) => set({ path: e.target.value })} />
              </label>
            </div>
          </>
        )}

        <div className="flex items-center gap-space-3">
          <Button
            className="bg-gradient-cta text-white hover:opacity-90"
            disabled={!dirty || saveMutation.isPending}
            onClick={() => saveMutation.mutate(form)}
          >
            保存
          </Button>
          {dirty && (
            <Button variant="ghost" onClick={() => setForm(formOf(resource))}>
              放弃修改
            </Button>
          )}
        </div>
      </div>

      {/* 完整配置摊开给人看：表单只接管了少数几个字段，Agent 运行时真正读
          的那些（component_type / tool_type / 参数 schema）在这里能核对。 */}
      <details className="rounded-lg border border-border bg-surface p-space-4">
        <summary className="text-label-md cursor-pointer text-ink-900">完整配置</summary>
        <pre className="text-caption mt-space-3 overflow-x-auto rounded-md bg-surface-muted p-space-3 text-ink-700">
          {JSON.stringify(resource.config, null, 2)}
        </pre>
        <p className="text-caption mt-space-2 text-ink-500">
          凭证字段不会出现在这里（也不会出现在任何接口响应里）。保存时不填即表示不改。
        </p>
      </details>
    </div>
  )
}
