import { Link } from 'react-router-dom'
import { Lock } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Ref } from '@/components/common/Page'
import type { components } from '@/lib/api/schema'

type ListingSummary = components['schemas']['ListingSummary']

/**
 * A listing card.
 *
 * What a person is deciding here is "do I trust this enough to subscribe",
 * so the card leads with the name and what it does, and puts the two facts
 * that answer that question — who wrote it, how many people already use it
 * — on the line above the action. The black-box mark is stated plainly
 * rather than as a decorative badge: it is a real limit on what you get,
 * and pretending otherwise would be a nasty surprise later.
 */
export function ListingCard({
  listing,
  onSubscribe,
}: {
  listing: ListingSummary
  onSubscribe: (listing: ListingSummary) => void
}) {
  return (
    <article className="flex h-full flex-col rounded-lg border border-border bg-surface transition-colors hover:border-border-strong">
      <div className="flex flex-1 flex-col gap-space-3 p-space-5">
        <div className="flex items-center justify-between gap-space-2">
          <Ref tone="blueprint">{listing.resource_type}</Ref>
          <span className="text-caption inline-flex items-center gap-1 text-ink-500" title="黑盒分发：订阅后可以运行，但看不到内部定义">
            <Lock className="size-3" aria-hidden />
            黑盒
          </span>
        </div>

        <Link
          to={`/marketplace/listing/${listing.listing_ref}`}
          className="text-display-md text-ink-900 hover:underline"
        >
          {listing.display_meta.display_name}
        </Link>

        <p className="text-body-sm line-clamp-3 flex-1 text-ink-700">
          {listing.display_meta.description}
        </p>

        <div className="flex items-center gap-space-2 border-t border-border pt-space-3">
          <span
            aria-hidden
            className="text-caption flex size-5 shrink-0 items-center justify-center rounded-full bg-surface-muted text-ink-700"
          >
            {listing.author.display_name.slice(0, 1).toUpperCase()}
          </span>
          <span className="text-caption min-w-0 flex-1 truncate text-ink-500">
            {listing.author.display_name}
          </span>
          <span className="text-caption tabular shrink-0 text-ink-500">
            v{listing.version} · {listing.subscriber_count} 人订阅
          </span>
        </div>
      </div>

      <div className="border-t border-border px-space-5 py-space-3">
        {listing.subscribed ? (
          <div className="flex items-center justify-between gap-space-3">
            <span className="text-caption inline-flex items-center gap-1.5 text-moss">
              <span aria-hidden className="size-1.5 rounded-full bg-moss" />
              已订阅 v{listing.version}
            </span>
            <Button asChild variant="outline" size="sm">
              <Link to={`/marketplace/listing/${listing.listing_ref}`}>打开</Link>
            </Button>
          </div>
        ) : (
          <Button size="sm" className="w-full" onClick={() => onSubscribe(listing)}>
            订阅 v{listing.version}
          </Button>
        )}
      </div>
    </article>
  )
}
