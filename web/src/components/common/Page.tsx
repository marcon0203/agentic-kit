import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

/**
 * Page scaffolding.
 *
 * Every centre in this app used to open the same way — big title, tab row,
 * a row of three big numbers, then a dashed empty box and a screenful of
 * nothing. That template said the same thing on six different pages, which
 * meant it said nothing on any of them.
 *
 * These pieces replace it. A page states what it is and what you can do
 * with it on one line, then goes straight to the work. Figures appear only
 * where a figure is the point, and they sit inline on a rule rather than
 * floating in cards of their own.
 */

/**
 * The page masthead: eyebrow, title, and one sentence saying what this
 * centre is for, with primary actions on the right of the same line.
 */
export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
  className,
}: {
  eyebrow: string
  title: string
  description?: string
  actions?: ReactNode
  className?: string
}) {
  return (
    <header
      className={cn('flex flex-wrap items-end justify-between gap-space-5 pb-space-5', className)}
    >
      <div className="flex min-w-0 max-w-[62ch] flex-col gap-space-2">
        <span className="text-eyebrow text-ink-500">{eyebrow}</span>
        <h1 className="text-display-lg text-ink-900">{title}</h1>
        {description && <p className="text-body-md text-ink-700">{description}</p>}
      </div>
      {actions ? <div className="flex shrink-0 items-center gap-space-3">{actions}</div> : null}
    </header>
  )
}

/**
 * A labelled band of the page. The rule under the label is the only
 * decoration allowed here — it does the job a card border used to do, at a
 * fraction of the visual weight.
 */
export function Section({
  title,
  aside,
  children,
  className,
}: {
  title: string
  aside?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section className={cn('flex flex-col gap-space-4', className)}>
      {/* items-center, not items-baseline: an aside is often a button, and
          sitting a 36px control on the text baseline drops it through the
          rule below. */}
      <div className="flex min-h-9 items-center justify-between gap-space-4 border-b border-border pb-space-3">
        <h2 className="text-display-sm text-ink-900">{title}</h2>
        {aside}
      </div>
      {children}
    </section>
  )
}

/**
 * A figure with its label. Monospace and tabular so a row of them aligns on
 * the decimal, which is the only reason to put numbers side by side.
 */
export function Figure({
  value,
  label,
  tone = 'ink',
  className,
}: {
  value: string
  label: string
  tone?: 'ink' | 'blueprint' | 'signal' | 'moss' | 'rust'
  className?: string
}) {
  const toneClass = {
    ink: 'text-ink-900',
    blueprint: 'text-blueprint',
    signal: 'text-signal',
    moss: 'text-moss',
    rust: 'text-rust',
  }[tone]

  return (
    <div className={cn('flex flex-col gap-space-1', className)}>
      <span className={cn('text-figure', toneClass)}>{value}</span>
      <span className="text-caption text-ink-500">{label}</span>
    </div>
  )
}

/**
 * A row of figures, separated by rules rather than boxed in cards.
 */
export function FigureRow({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div
      className={cn(
        'grid gap-px overflow-hidden rounded-lg border border-border bg-border sm:grid-cols-3',
        className,
      )}
    >
      {children}
    </div>
  )
}

export function FigureCell({ children }: { children: ReactNode }) {
  return <div className="bg-surface px-space-5 py-space-4">{children}</div>
}

/**
 * A plain content panel. One border, no shadow — elevation is reserved for
 * things that genuinely float (popovers, modals, a dragged node).
 */
export function Panel({
  children,
  className,
  padded = true,
}: {
  children: ReactNode
  className?: string
  padded?: boolean
}) {
  return (
    <div
      className={cn(
        'rounded-lg border border-border bg-surface',
        padded && 'p-space-5',
        className,
      )}
    >
      {children}
    </div>
  )
}

/**
 * An identifier: ref, version, run_id, node name. Always monospace, always
 * visually distinct from prose, because these are strings a user copies and
 * matches character by character.
 */
export function Ref({
  children,
  tone = 'default',
  className,
}: {
  children: ReactNode
  tone?: 'default' | 'muted' | 'blueprint'
  className?: string
}) {
  const toneClass = {
    default: 'bg-surface-muted text-ink-900',
    muted: 'bg-transparent text-ink-500',
    blueprint: 'bg-blueprint-tint text-blueprint',
  }[tone]

  return (
    <code
      className={cn(
        'text-ref inline-flex max-w-full items-center truncate rounded-xs px-1.5 py-0.5',
        toneClass,
        className,
      )}
    >
      {children}
    </code>
  )
}

/**
 * The tab row used by centres with sub-views. Underline rather than pill:
 * an underline is a position marker, which is what a tab is; a filled pill
 * reads as a button and invites a click on the tab you are already on.
 */
export function TabRail({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div
      role="tablist"
      className={cn('flex items-center gap-space-6 border-b border-border', className)}
    >
      {children}
    </div>
  )
}

export function TabRailItem({
  active,
  onClick,
  children,
  count,
}: {
  active: boolean
  onClick: () => void
  children: ReactNode
  count?: number
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        'text-label-md -mb-px flex items-center gap-space-2 border-b-2 px-0.5 pb-space-3 pt-space-1 transition-colors',
        active
          ? 'border-blueprint text-ink-900'
          : 'border-transparent text-ink-500 hover:text-ink-900',
      )}
    >
      {children}
      {typeof count === 'number' ? (
        <span className="text-caption tabular text-ink-500">{count}</span>
      ) : null}
    </button>
  )
}

/**
 * A filter chip row. Deliberately *not* the underlined TabRail: a tab
 * switches which view you are in, a filter narrows what is inside the view
 * you are already in. Stacking two identical-looking rails made the page
 * read as two levels of navigation when only the first one is.
 */
export function FilterChips({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div role="group" aria-label="筛选" className={cn('flex flex-wrap items-center gap-space-2', className)}>
      {children}
    </div>
  )
}

export function FilterChip({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: ReactNode
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        'text-caption rounded-full border px-space-3 py-1 transition-colors',
        active
          ? 'border-blueprint bg-blueprint-tint text-blueprint'
          : 'border-border-strong text-ink-700 hover:border-ink-500 hover:text-ink-900',
      )}
    >
      {children}
    </button>
  )
}
