import { useNavigate } from 'react-router-dom'
import { ArrowRight, Download, Star } from 'lucide-react'

import type { components } from '@/lib/api/schema'

type MarketSkill = components['schemas']['MarketSkill']

/**
 * Skill 市场（外部源）的卡片：名称 + 简介 + 主题 + 数据。悬停时底部浮出
 * "查看详情"，进详情页看用法、来源、作者和更新记录。
 */
export function MarketSkillCard({ skill }: { skill: MarketSkill }) {
  const navigate = useNavigate()

  return (
    <div className="group relative flex min-h-[9.5rem] cursor-pointer flex-col gap-space-2 overflow-hidden rounded-lg border border-border bg-surface p-space-4 transition-colors hover:border-border-strong">
      <button
        type="button"
        className="absolute inset-0 z-10"
        aria-label={`查看 ${skill.name} 详情`}
        onClick={() => navigate(`/apps/skill/market/${skill.source_id}/${skill.slug}`)}
      />
      <span className="text-body-md truncate font-medium text-ink-900">{skill.name}</span>
      <span className="text-body-sm line-clamp-2 text-ink-500">
        {skill.summary || '上游没有提供简介。'}
      </span>
      <span className="text-caption mt-auto flex items-center gap-space-3 text-ink-500">
        {skill.version && <span className="tabular">v{skill.version}</span>}
        <span className="inline-flex items-center gap-1">
          <Star className="size-3" aria-hidden />
          <span className="tabular">{skill.stars}</span>
        </span>
        <span className="inline-flex items-center gap-1">
          <Download className="size-3" aria-hidden />
          <span className="tabular">{skill.downloads}</span>
        </span>
        <span className="truncate">来自 {skill.source_name}</span>
      </span>
      {skill.topics.length > 0 && (
        <span className="flex flex-wrap gap-space-1">
          {skill.topics.slice(0, 3).map((t) => (
            <span key={t} className="text-caption rounded-full bg-surface-muted px-space-2 py-0.5 text-ink-700">
              {t}
            </span>
          ))}
        </span>
      )}

      {/* 悬停浮出的查看详情条 */}
      <span className="pointer-events-none absolute inset-x-0 bottom-0 z-20 flex translate-y-full items-center justify-center gap-space-1 rounded-b-lg bg-blueprint py-2 text-body-sm font-medium text-white opacity-0 transition-all duration-150 group-hover:translate-y-0 group-hover:opacity-100">
        查看详情
        <ArrowRight className="size-3.5" aria-hidden />
      </span>
    </div>
  )
}
