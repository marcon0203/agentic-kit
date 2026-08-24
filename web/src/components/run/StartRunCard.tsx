import { useEffect, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import { useHasModelProvider } from '@/lib/models/useHasModelProvider'

/**
 * A minimal run-trigger — the real Bundle picker lands with spec-15/17's
 * application management pages. This exists now so spec-14's "发起运行后
 *自动跳转" is reachable and testable without waiting on those.
 */
export function StartRunCard() {
  const navigate = useNavigate()
  const location = useLocation()
  const prefill = (location.state as { quickStartBundleRef?: string } | null)?.quickStartBundleRef
  const { hasProvider, isLoading: providerLoading } = useHasModelProvider()

  const [bundleRef, setBundleRef] = useState(prefill ?? '')
  const [input, setInput] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  useEffect(() => {
    if (prefill) setBundleRef(prefill)
  }, [prefill])

  async function submit() {
    setPending(true)
    setError(null)
    try {
      const res = await apiClient.POST('/runs', {
        body: { bundle_ref: bundleRef, input: { requirements: input } },
        params: { header: { 'Idempotency-Key': crypto.randomUUID() } },
      })
      const run = unwrap<{ run_id: string }>(res)
      navigate(`/runs/${run.run_id}`, { state: { inputText: input } })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '发起运行失败，请稍后重试')
    } finally {
      setPending(false)
    }
  }

  const blocked = !providerLoading && !hasProvider

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-display-md">发起运行</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-space-4">
        {blocked && (
          <p className="text-body-sm rounded-sm border border-signal bg-signal-tint px-space-4 py-space-3 text-ink-700">
            尚未接入任何模型 Provider，
            <Link to="/models" className="text-blueprint hover:underline">
              前往模型中心接入
            </Link>
            后才能运行。
          </p>
        )}
        <div className="flex flex-col gap-space-2">
          <label htmlFor="bundle-ref" className="text-label-md text-ink-700">
            Bundle ref
          </label>
          <Input
            id="bundle-ref"
            value={bundleRef}
            onChange={(e) => setBundleRef(e.target.value)}
            placeholder="web-app-builder"
          />
        </div>
        <div className="flex flex-col gap-space-2">
          <label htmlFor="run-input" className="text-label-md text-ink-700">
            需求描述
          </label>
          <Textarea
            id="run-input"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="做一个待办事项 Web 应用"
            rows={3}
          />
        </div>
        {error && (
          <p role="alert" className="text-body-sm text-rust">
            {error}
          </p>
        )}
        <Button
          disabled={pending || !bundleRef || !input || blocked}
          title={blocked ? '请先在模型中心接入模型 Provider' : undefined}
          onClick={submit}
          className="self-start"
        >
          {pending ? '发起中…' : '运行'}
        </Button>
      </CardContent>
    </Card>
  )
}
