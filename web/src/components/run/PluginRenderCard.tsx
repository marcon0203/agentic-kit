import { useEffect, useRef } from 'react'

import type { TimelineEntry } from '@/lib/runs/timeline'

type RenderEntry = Extract<TimelineEntry, { kind: 'render' }>

/**
 * The asset endpoint is unauthenticated by design (spec-20 §4.2/§5.2) —
 * the iframe's `src` navigation never carries the app's Authorization
 * header, so the endpoint can't require one. It's under /api/v1 rather
 * than a truly separate origin for now; real cross-origin isolation is a
 * deployment-level reverse-proxy concern (a distinct asset subdomain),
 * not something this component can arrange for itself.
 */
function assetUrl(plugin: string, version: string, path: string): string {
  return `/api/v1/plugins/assets/${encodeURIComponent(plugin)}/${encodeURIComponent(version)}/${path}`
}

/**
 * ds-plugin-render-card — a plugin renderer's iframe, inline in the
 * conversation next to the node that triggered it. Sandboxed with
 * `allow-scripts` only: no `allow-same-origin`, so the iframe's own
 * content is treated as a genuinely untrusted third origin even though it
 * happens to be served from this app's own host (spec-20 §4.2).
 *
 * The postMessage bridge is intentionally minimal for P2: one message
 * posted once the iframe loads, carrying `data` and a resize hint. A
 * plugin that wants a richer host API (calling back into a tool, for
 * instance) is a P6 SDK concern — the wire contract here is deliberately
 * small enough that a hand-written `ui/chart.html` can consume it with no
 * library at all.
 */
export function PluginRenderCard({ entry }: { entry: RenderEntry }) {
  const iframeRef = useRef<HTMLIFrameElement>(null)

  useEffect(() => {
    function onMessage(ev: MessageEvent) {
      if (ev.source !== iframeRef.current?.contentWindow) return
      const msg = ev.data as { type?: string; height?: number } | undefined
      if (msg?.type === 'agentic-kit:resize' && iframeRef.current && typeof msg.height === 'number') {
        iframeRef.current.style.height = `${Math.max(80, Math.min(msg.height, 1200))}px`
      }
    }
    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [])

  function onLoad() {
    iframeRef.current?.contentWindow?.postMessage(
      { type: 'agentic-kit:init', resourceUri: entry.resourceUri, data: entry.data },
      '*',
    )
  }

  return (
    <div className="overflow-hidden rounded-lg border border-border bg-surface">
      <div className="flex items-center gap-space-2 border-b border-border px-space-4 py-space-2">
        <span aria-hidden className="size-2 rounded-full bg-blueprint" />
        <span className="text-label-sm text-ink-700">
          {entry.node} · {entry.plugin}/{entry.renderer}
        </span>
      </div>
      <iframe
        ref={iframeRef}
        title={`${entry.plugin}/${entry.renderer}`}
        src={assetUrl(entry.plugin, entry.version, entry.entry)}
        sandbox="allow-scripts"
        onLoad={onLoad}
        className="h-[320px] w-full border-0"
      />
    </div>
  )
}
