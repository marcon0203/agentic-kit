import { PageHeader } from '@/components/common/Page'

/**
 * Shell for a route whose real page is not built yet. It says so plainly
 * rather than showing a fake dashboard — a placeholder that pretends to be
 * finished is worse than an honest one.
 */
export function PagePlaceholder({ title, note }: { title: string; note: string }) {
  return (
    <div className="flex flex-col gap-space-6">
      <PageHeader eyebrow="COMING SOON" title={title} description={note} />
      <div className="rounded-lg border border-border bg-surface px-space-6 py-space-9">
        <p className="text-body-md text-ink-700">这个页面还没有建好，导航先留在这里。</p>
      </div>
    </div>
  )
}
