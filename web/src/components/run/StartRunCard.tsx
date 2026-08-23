import { useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'

/**
 * A minimal run-trigger — the real Bundle picker lands with spec-15/17's
 * application management pages. This exists now so spec-14's "发起运行后
 *自动跳转" is reachable and testable without waiting on those.
 */
export function StartRunCard() {
  const navigate = useNavigate()
  const [bundleRef, setBundleRef] = useState('')
  const [input, setInput] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

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

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-headline-sm">发起运行</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-space-4">
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
          <p role="alert" className="text-body-sm text-[var(--color-error)]">
            {error}
          </p>
        )}
        <Button disabled={pending || !bundleRef || !input} onClick={submit} className="self-start">
          {pending ? '发起中…' : '运行'}
        </Button>
      </CardContent>
    </Card>
  )
}
