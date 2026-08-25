import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Ref } from '@/components/common/Page'
import { EmptyRail } from '@/components/common/Rail'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { RegisterResourceDialog } from '@/components/resources/RegisterResourceDialog'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import { cn } from '@/lib/utils'
import { useFeatures } from '@/lib/features/useFeatures'
import type { components } from '@/lib/api/schema'

type ResourceType = components['schemas']['ResourceType']
type Resource = components['schemas']['Resource']

/* Each kind gets its own empty-state copy. "还没有资源" is the same sentence
   four times over and helps nobody; what a person needs to know is what this
   particular kind is for and what registering one would let them do. Also
   doubles as the label lookup AppsLayout's sidebar uses, so a kind's name is
   spelled once. */
export const RESOURCE_KINDS: {
  value: ResourceType
  label: string
  blank: { title: string; description: string; cta: string }
}[] = [
  {
    value: 'tool',
    label: '组件',
    blank: {
      title: '给 Agent 一件能用的工具',
      description:
        '组件是 Agent 能调用的外部能力：一个检索接口、一个内部服务、一个沙箱环境……注册后才能写进 Agent 的能力白名单。',
      cta: '注册组件',
    },
  },
  {
    value: 'skill',
    label: 'Skill',
    blank: {
      title: '沉淀一段可复用的做法',
      description: 'Skill 把一段固定的做事方式打包，让多个 Agent 共用同一套步骤，而不是各写各的提示词。',
      cta: '注册 Skill',
    },
  },
  {
    value: 'mcp',
    label: 'MCP Server',
    blank: {
      title: '接入一台 MCP Server',
      description:
        '登记地址与凭证后平台会立刻探测一次连通性，结果显示在这里。凭证加密落库，任何响应都不会带出来。',
      cta: '接入 MCP Server',
    },
  },
  {
    value: 'knowledge_base',
    label: '知识库',
    blank: {
      title: '让 Agent 有资料可查',
      description: '知识库登记后可以被 Agent 引用，回答时从这里检索，而不是全靠模型自己记得。',
      cta: '注册知识库',
    },
  },
  {
    value: 'memory',
    label: '记忆库',
    blank: {
      title: '让对话记住上一次',
      description:
        '记忆库登记后，同一个账号下的运行会把对话写进这里；Agent 勾选 load_memory / preload_memory 内置工具即可检索，重启进程也不会丢。',
      cta: '注册记忆库',
    },
  },
]

/**
 * 一种资源类型的注册与列表页——Tool/Skill/MCP Server/知识库/记忆库各自是
 * 应用广场二级菜单里独立的一项，而不是同一个页面里的 Tab：每种资源的配置
 * 项差别不小（MCP 有连通性探测、知识库有向量模型配置），揉在一个 Tab 页
 * 里切换只会让人以为它们是同一件事的四种视图。
 */
export function ResourceKindPage({ type }: { type: ResourceType }) {
  const [search, setSearch] = useState('')
  const [registerOpen, setRegisterOpen] = useState(false)
  const [toggleError, setToggleError] = useState<string | null>(null)
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { knowledgeBaseEnabled, skillUploadEnabled, isLoading: featuresLoading } = useFeatures()

  // MCP Server has its own multi-field page (URL, header list, a 检测
  // button that needs room to show probed results), and Skill only
  // accepts a zip upload (spec-05a) — neither fits the generic
  // ref+display_name+JSON dialog, so their CTAs route to a dedicated page
  // instead of opening the dialog.
  function openRegister() {
    if (type === 'mcp') {
      navigate('/apps/mcp/new')
      return
    }
    if (type === 'skill') {
      navigate('/apps/skill/new')
      return
    }
    setRegisterOpen(true)
  }

  const query = useQuery({
    queryKey: ['resources', type],
    queryFn: async () =>
      unwrap<{ items: Resource[]; has_more: boolean }>(
        await apiClient.GET('/resources', { params: { query: { type } } }),
      ),
    enabled: type !== 'knowledge_base' || knowledgeBaseEnabled,
  })

  if (type === 'knowledge_base' && !featuresLoading && !knowledgeBaseEnabled) {
    return (
      <EmptyRail
        title="知识库功能未启用"
        description="知识库依赖 Milvus（向量检索）和 Elasticsearch（关键词检索）——这台服务器的配置文件里 KB_ENABLED 是关闭的，找管理员在部署配置里打开并填好两边的连接地址。"
      />
    )
  }

  // Skill zip upload needs an object store — a deployment that never sets
  // OSS_* still shows the list (existing Skills stay visible), but the CTA
  // is disabled with an explanation rather than opening an upload page that
  // will 400 on submit.
  const skillUploadBlocked = type === 'skill' && !featuresLoading && !skillUploadEnabled

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
      queryClient.invalidateQueries({ queryKey: ['resources', type] })
    } catch (err) {
      setToggleError(err instanceof ApiError ? err.message : '操作没能完成，请再试一次')
    }
  }

  const kind = RESOURCE_KINDS.find((k) => k.value === type)!
  const items = query.data?.items ?? []
  const filtered = search
    ? items.filter((r) => r.ref.includes(search) || (r.display_name ?? '').includes(search))
    : items

  return (
    <div className="flex flex-col gap-space-6">
      <div className="flex items-center justify-end gap-space-3">
        {skillUploadBlocked && (
          <span className="text-caption text-ink-500">未配置对象存储（OSS_*），Skill 上传暂不可用</span>
        )}
        <Button
          className="bg-gradient-cta text-white hover:opacity-90"
          onClick={openRegister}
          disabled={skillUploadBlocked}
        >
          {kind.blank.cta}
        </Button>
      </div>

      {items.length > 0 && (
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="按 ref 或名称筛选"
          className="max-w-xs"
        />
      )}

      {toggleError && (
        <p role="alert" className="text-body-sm text-rust">
          {toggleError}
        </p>
      )}

      {query.isLoading && <ListSkeleton />}

      {query.isError && <ErrorPanel message="资源列表没能加载出来" onRetry={() => query.refetch()} />}

      {query.isSuccess && items.length === 0 && (
        <EmptyRail
          title={skillUploadBlocked ? 'Skill 上传暂不可用' : kind.blank.title}
          description={
            skillUploadBlocked
              ? '这台服务器没有配置对象存储（OSS_*），Skill 上传依赖它来存放 zip 包的内容——找管理员在部署配置里补上。'
              : kind.blank.description
          }
          action={
            !skillUploadBlocked && (
              <Button size="sm" className="bg-gradient-cta text-white hover:opacity-90" onClick={openRegister}>
                {kind.blank.cta}
              </Button>
            )
          }
        />
      )}

      {query.isSuccess && items.length > 0 && filtered.length === 0 && (
        <EmptyRail
          title={`没有 ref 或名称包含「${search}」的${kind.label}`}
          description="筛选只匹配 ref 和显示名称，不搜索配置内容。"
          action={
            <Button variant="outline" size="sm" onClick={() => setSearch('')}>
              清除筛选
            </Button>
          }
        />
      )}

      {filtered.length > 0 && (
        <ul className="overflow-hidden rounded-lg border border-border bg-surface">
          {filtered.map((r) => (
            <li
              key={r.id}
              className="flex items-center gap-space-4 border-b border-border px-space-5 py-space-3 last:border-0"
            >
              <span
                aria-hidden
                className={cn(
                  'size-2 shrink-0 rounded-full',
                  r.status === 1 ? 'bg-moss' : 'bg-border-strong',
                )}
              />
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="flex items-center gap-space-3">
                  <Ref>{r.ref}</Ref>
                  {r.display_name && (
                    <span className="text-body-sm truncate text-ink-700">{r.display_name}</span>
                  )}
                </span>
                {r.health && r.health !== 'unknown' && (
                  <span
                    className={cn(
                      'text-caption',
                      r.health === 'healthy' ? 'text-moss' : 'text-rust',
                    )}
                  >
                    {r.health === 'healthy' ? '上次探测：连接正常' : '上次探测：连不上，检查地址与凭证'}
                  </span>
                )}
              </span>
              <span
                className={cn(
                  'text-caption w-12 shrink-0 text-right',
                  r.status === 1 ? 'text-moss' : 'text-ink-500',
                )}
              >
                {r.status === 1 ? '已启用' : '已停用'}
              </span>
              <Button variant="outline" size="sm" onClick={() => toggleStatus(r)}>
                {r.status === 1 ? '停用' : '启用'}
              </Button>
            </li>
          ))}
        </ul>
      )}

      <RegisterResourceDialog
        type={type}
        open={registerOpen}
        onOpenChange={setRegisterOpen}
        onCreated={() => queryClient.invalidateQueries({ queryKey: ['resources', type] })}
      />
    </div>
  )
}
