import { ChevronLeft, ChevronRight } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'

export const PAGE_SIZES = [12, 24, 48]

/**
 * 页码窗口：7 页以内全列出来，再多就首尾各留一个、当前页左右各留一个，中
 * 间用省略号补上——否则一个几十页的列表会把整行页码撑到换行。
 */
function pageWindow(page: number, pageCount: number): (number | '…')[] {
  if (pageCount <= 7) return Array.from({ length: pageCount }, (_, i) => i + 1)

  const out: (number | '…')[] = [1]
  const start = Math.max(2, page - 1)
  const end = Math.min(pageCount - 1, page + 1)
  if (start > 2) out.push('…')
  for (let i = start; i <= end; i++) out.push(i)
  if (end < pageCount - 1) out.push('…')
  out.push(pageCount)
  return out
}

/** 卡片墙通用的分页行：页码 + 每页条数。 */
export function Pagination({
  page,
  pageCount,
  pageSize,
  onPageChange,
  onPageSizeChange,
}: {
  page: number
  pageCount: number
  pageSize: number
  onPageChange: (page: number) => void
  onPageSizeChange: (size: number) => void
}) {
  return (
    <nav aria-label="分页" className="flex items-center justify-end gap-space-2">
      <Button
        variant="ghost"
        size="sm"
        className="text-ink-500 size-8 p-0"
        disabled={page <= 1}
        aria-label="上一页"
        onClick={() => onPageChange(page - 1)}
      >
        <ChevronLeft className="size-4" aria-hidden />
      </Button>

      {pageWindow(page, pageCount).map((entry, i) =>
        entry === '…' ? (
          <span key={`gap-${i}`} className="text-body-sm px-1 text-ink-500">
            …
          </span>
        ) : (
          <button
            key={entry}
            type="button"
            aria-current={entry === page ? 'page' : undefined}
            onClick={() => onPageChange(entry)}
            className={cn(
              'text-body-sm size-8 rounded-md border transition-colors',
              entry === page
                ? 'border-primary bg-blueprint-tint text-blueprint'
                : 'border-transparent text-ink-700 hover:bg-surface-muted',
            )}
          >
            {entry}
          </button>
        ),
      )}

      <Button
        variant="ghost"
        size="sm"
        className="text-ink-500 size-8 p-0"
        disabled={page >= pageCount}
        aria-label="下一页"
        onClick={() => onPageChange(page + 1)}
      >
        <ChevronRight className="size-4" aria-hidden />
      </Button>

      <Select value={String(pageSize)} onValueChange={(v) => onPageSizeChange(Number(v))}>
        <SelectTrigger className="h-8 w-[110px]" aria-label="每页条数">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {PAGE_SIZES.map((size) => (
            <SelectItem key={size} value={String(size)}>
              {size} 条/页
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </nav>
  )
}
