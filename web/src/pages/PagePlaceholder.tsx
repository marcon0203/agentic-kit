import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

/**
 * Shared shell for a section whose real page lands in a later spec
 * (spec-14~19) — keeps the route table and nav wired up now without
 * pretending the page is finished.
 */
export function PagePlaceholder({ title, note }: { title: string; note: string }) {
  return (
    <div className="flex flex-col gap-space-6">
      <h1 className="text-headline-md text-ink-900">{title}</h1>
      <Card className="border-dashed">
        <CardHeader>
          <CardTitle className="text-headline-sm">页面建设中</CardTitle>
        </CardHeader>
        <CardContent className="text-body-md text-ink-700">{note}</CardContent>
      </Card>
    </div>
  )
}
