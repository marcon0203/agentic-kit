import { Link } from 'react-router-dom'
import { Lock } from 'lucide-react'

import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import type { components } from '@/lib/api/schema'

type ListingSummary = components['schemas']['ListingSummary']

/** ds-listing-card — design-system.md §八: cover/type chip → author row →
 * title(title-card, serif) → summary → tags → subscriber metric →
 * exactly one primary action. Hover moves up 4px, never scales. */
export function ListingCard({
  listing,
  onSubscribe,
}: {
  listing: ListingSummary
  onSubscribe: (listing: ListingSummary) => void
}) {
  return (
    <Card className="flex h-full flex-col transition-all duration-150 hover:-translate-y-1 hover:shadow-md">
      <CardHeader className="flex-row items-start justify-between gap-space-2">
        <Badge variant="outline">{listing.resource_type}</Badge>
        <span className="text-caption inline-flex items-center gap-1 text-ink-500">
          <Lock className="size-3" aria-hidden />
          黑盒
        </span>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col gap-space-3">
        <Link to={`/marketplace/listing/${listing.listing_ref}`} className="hover:underline">
          <h3 className="text-title-card text-ink-900">{listing.display_meta.display_name}</h3>
        </Link>
        <p className="text-body-sm line-clamp-3 flex-1 text-ink-700">{listing.display_meta.description}</p>
        <div className="flex items-center gap-space-2">
          <div aria-hidden className="flex size-5 items-center justify-center rounded-full bg-surface-muted text-caption text-ink-700">
            {listing.author.display_name.slice(0, 1).toUpperCase()}
          </div>
          <span className="text-caption text-ink-500">
            {listing.author.display_name} · v{listing.version} · {listing.subscriber_count} 人订阅
          </span>
        </div>

        {listing.subscribed ? (
          <div className="flex items-center gap-space-2">
            <Badge>已订阅</Badge>
            <Button asChild variant="secondary" size="sm" className="ml-auto">
              <Link to={`/marketplace/listing/${listing.listing_ref}`}>进入使用</Link>
            </Button>
          </div>
        ) : (
          <Button size="sm" onClick={() => onSubscribe(listing)}>
            订阅 v{listing.version}
          </Button>
        )}
      </CardContent>
    </Card>
  )
}
