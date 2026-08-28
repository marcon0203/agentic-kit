import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery } from '@tanstack/react-query'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { toast } from 'sonner'
import { ArrowLeft, Download, ExternalLink, Star } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { Ref, TabRail, TabRailItem } from '@/components/common/Page'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type MarketSkillDetail = components['schemas']['MarketSkillDetail']

/**
 * 把 SKILL.md 开头的 YAML front-matter（--- … ---）拆出来：元数据部分由
 * parseFrontMatter 变成两列表格，剩余正文走 Markdown 渲染。
 */
function splitFrontMatter(usage: string): { meta: string; body: string } {
  const m = usage.match(/^\s*---\r?\n([\s\S]*?)\r?\n---\r?\n?/)
  return m ? { meta: m[1], body: usage.slice(m[0].length) } : { meta: '', body: usage }
}

/**
 * 极简 YAML 展平：只处理 SKILL.md front-matter 里实际出现的形态——
 * `key: value`、嵌套块（缩进）、`- item` 列表。键按嵌套路径拼成
 * `metadata.emoji`，列表项并成一个值。解析不了的行直接跳过。
 */
function parseFrontMatter(block: string): [string, string][] {
  const rows: [string, string][] = []
  const path: string[] = []
  let lastKey = ''
  for (const raw of block.split(/\r?\n/)) {
    if (!raw.trim()) continue
    if (/^\s*-\s+/.test(raw)) {
      const item = raw.trim().replace(/^-\s+/, '').replace(/^["']|["']$/g, '')
      const row = rows.find((r) => r[0] === lastKey)
      if (row) row[1] = row[1] ? `${row[1]}、${item}` : item
      continue
    }
    const kv = raw.trim().match(/^([^:]+):\s*(.*)$/)
    if (!kv) continue
    const key = kv[1].trim()
    const value = kv[2].replace(/^["']|["']$/g, '').trim()
    path.length = Math.floor((raw.length - raw.trimStart().length) / 2)
    path.push(key)
    lastKey = path.join('.')
    if (value !== '') rows.push([lastKey, value])
  }
  return rows
}

/**
 * Skill 市场详情：用法（上游 SKILL.md 原文，Markdown 渲染）、来源、作者、
 * 更新记录——用法和更新记录是两块大内容，用 tab 切换而不是上下滚动。
 * 数据由后端缓存快照 + 回源补全，上游临时不可达时 usage/作者/版本列表
 * 可能缺省，页面按字段有无逐段渲染。
 */
export function SkillMarketDetailPage() {
  const { sourceId, slug } = useParams<{ sourceId: string; slug: string }>()
  const navigate = useNavigate()
  const [tab, setTab] = useState<'usage' | 'versions'>('usage')

  const installMutation = useMutation({
    mutationFn: async () => {
      await apiClient.POST('/skill-market/{source_id}/{slug}/install', {
        params: { path: { source_id: sourceId!, slug: slug! } },
      })
    },
    onSuccess: () => {
      toast.success('已安装到我的 Skill')
      navigate('/apps/skill')
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : '安装没能完成，请再试一次'),
  })

  const query = useQuery({
    queryKey: ['skill-market', sourceId, slug],
    queryFn: async () =>
      unwrap<MarketSkillDetail>(
        await apiClient.GET('/skill-market/{source_id}/{slug}', {
          params: { path: { source_id: sourceId!, slug: slug! } },
        }),
      ),
    enabled: !!sourceId && !!slug,
  })

  if (query.isLoading) {
    return <ListSkeleton rows={6} />
  }
  if (query.isError) {
    return <ErrorPanel message="Skill 详情没能加载出来" onRetry={() => query.refetch()} />
  }

  const skill = query.data
  if (!skill) return null

  return (
    <div className="flex flex-col gap-space-6">
      <div className="flex flex-wrap items-center justify-between gap-space-3">
        <Button variant="ghost" size="sm" onClick={() => navigate('/apps/skill')}>
          <ArrowLeft aria-hidden />
          返回市场
        </Button>
        <div className="flex items-center gap-space-2">
          {skill.upstream_url && (
            <Button variant="outline" size="sm" onClick={() => window.open(skill.upstream_url, '_blank', 'noreferrer')}>
              <ExternalLink aria-hidden />
              去源站查看
            </Button>
          )}
          <Button
            className="bg-gradient-cta text-white hover:opacity-90"
            size="sm"
            disabled={installMutation.isPending}
            onClick={() => installMutation.mutate()}
          >
            <Download aria-hidden />
            {installMutation.isPending ? '安装中…' : '安装到我的 Skill'}
          </Button>
        </div>
      </div>

      <div className="flex flex-col gap-space-3">
        <div className="flex flex-wrap items-center gap-space-3">
          <h1 className="text-display-md text-ink-900">{skill.name}</h1>
          {skill.topics.map((t) => (
            <span
              key={t}
              className="text-caption rounded-full bg-surface-muted px-space-2 py-0.5 text-ink-700"
            >
              {t}
            </span>
          ))}
        </div>
        {/* 元信息一行排完：版本/数据/更新时间之后接来源和作者，免得每项
            独占一块把首屏撑高。 */}
        <div className="text-body-sm flex flex-wrap items-center gap-x-space-4 gap-y-space-2 text-ink-500">
          <Ref>{skill.slug}</Ref>
          {skill.version && <span className="tabular">v{skill.version}</span>}
          {skill.license && <span>{skill.license}</span>}
          <span className="inline-flex items-center gap-1">
            <Star className="size-3.5" aria-hidden />
            <span className="tabular">{skill.stars}</span>
          </span>
          <span className="inline-flex items-center gap-1">
            <Download className="size-3.5" aria-hidden />
            <span className="tabular">{skill.downloads}</span>
          </span>
          {skill.updated_at && <span>更新于 {new Date(skill.updated_at).toLocaleDateString()}</span>}
          <span>
            来源{' '}
            <a
              href={skill.source_base_url}
              target="_blank"
              rel="noreferrer"
              className="font-medium text-blueprint hover:underline"
            >
              {skill.source_name}
            </a>
          </span>
          <span className="inline-flex items-center gap-1">
            作者
            {skill.owner?.avatar && (
              <img src={skill.owner.avatar} alt="" className="size-4 rounded-full object-cover" />
            )}
            <span className="text-ink-700">
              {skill.owner ? skill.owner.display_name || skill.owner.handle : '上游未提供'}
            </span>
          </span>
        </div>
      </div>

      <TabRail>
        <TabRailItem active={tab === 'usage'} onClick={() => setTab('usage')}>
          用法
        </TabRailItem>
        <TabRailItem active={tab === 'versions'} onClick={() => setTab('versions')}>
          更新记录
          {skill.versions.length > 0 ? (
            <span className="text-caption tabular text-ink-500">{skill.versions.length}</span>
          ) : null}
        </TabRailItem>
      </TabRail>

      {tab === 'usage' ? (
        skill.usage ? (
          (() => {
            const { meta, body } = splitFrontMatter(skill.usage)
            const metaRows = meta ? parseFrontMatter(meta) : []
            return (
              <div className="flex flex-col gap-space-6">
                {metaRows.length > 0 && (
                  <table className="text-body-sm w-full overflow-hidden rounded-lg border border-border bg-surface">
                    <tbody>
                      {metaRows.map(([k, v]) => (
                        <tr key={k} className="border-b border-border last:border-0">
                          <td className="text-caption w-56 bg-surface-muted px-space-4 py-space-2 align-top text-ink-500">
                            {k}
                          </td>
                          <td className="px-space-4 py-space-2 align-top break-all text-ink-900">{v}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}
                {body.trim() && (
                  <div className="text-body-sm prose prose-sm max-w-none prose-headings:text-ink-900 prose-p:text-ink-700 prose-a:text-blueprint prose-code:rounded-xs prose-code:bg-surface-muted prose-code:px-1 prose-code:py-0.5 prose-code:text-ink-900 prose-pre:bg-surface-muted prose-table:text-ink-700 prose-hr:border-transparent">
                    <Markdown remarkPlugins={[remarkGfm]}>{body}</Markdown>
                  </div>
                )}
              </div>
            )
          })()
        ) : (
          <p className="text-body-sm text-ink-500">
            {skill.summary || '上游没有提供用法说明。'}
          </p>
        )
      ) : skill.versions.length > 0 ? (
        <ol className="flex flex-col">
          {skill.versions.map((v) => (
            <li
              key={v.version}
              className="flex flex-col gap-space-1 border-b border-border py-space-3 last:border-0"
            >
              <span className="flex items-center gap-space-3">
                <span className="text-ref text-ink-900">v{v.version}</span>
                {v.created_at && <span className="text-caption text-ink-500">{v.created_at}</span>}
              </span>
              {v.changelog && (
                <p className="text-body-sm whitespace-pre-wrap text-ink-700">{v.changelog}</p>
              )}
            </li>
          ))}
        </ol>
      ) : (
        <p className="text-body-sm text-ink-500">上游没有提供版本历史，或暂时取不到。</p>
      )}
    </div>
  )
}
