import { useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'

import { Ref } from '@/components/common/Page'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import { useHasModelProvider } from '@/lib/models/useHasModelProvider'

/**
 * The chat page a run's "运行" button jumps straight into — composing the
 * first message is the start of the same conversation RunPage continues,
 * not a separate form buried below the app list.
 */
export function NewRunPage() {
  const [searchParams] = useSearchParams()
  const bundleRef = searchParams.get('bundle') ?? ''
  const navigate = useNavigate()
  const { hasProvider, isLoading: providerLoading } = useHasModelProvider()
  const blocked = !providerLoading && !hasProvider

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
      navigate(`/runs/${run.run_id}`, { state: { inputText: input }, replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '发起运行失败，请稍后重试')
    } finally {
      setPending(false)
    }
  }

  if (!bundleRef) {
    return (
      <div className="mx-auto flex max-w-[560px] flex-col items-center gap-space-4 py-space-11 text-center">
        <p className="text-body-md text-ink-700">没有指定要运行的应用。</p>
        <Button variant="outline" size="sm" onClick={() => navigate('/apps/bundles')}>
          返回应用管理
        </Button>
      </div>
    )
  }

  return (
    <div className="mx-auto flex max-w-[720px] flex-col gap-space-6 py-space-8">
      <div className="flex items-center gap-space-3">
        <Button variant="ghost" size="sm" onClick={() => navigate('/apps/bundles')}>
          <ArrowLeft className="size-4" aria-hidden />
          返回
        </Button>
        <span className="text-body-sm text-ink-500">运行</span>
        <Ref>{bundleRef}</Ref>
      </div>

      {blocked && (
        <p className="text-body-sm rounded-sm border border-signal bg-signal-tint px-space-4 py-space-3 text-ink-700">
          尚未接入任何模型 Provider，
          <Link to="/models" className="text-blueprint hover:underline">
            前往模型广场接入
          </Link>
          后才能运行。
        </p>
      )}

      <div className="flex flex-col gap-space-3 rounded-lg border border-border bg-surface p-space-6">
        <label htmlFor="run-input" className="text-label-md text-ink-700">
          需求描述
        </label>
        <Textarea
          id="run-input"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="做一个待办事项 Web 应用"
          rows={6}
          autoFocus
        />
        {error && (
          <p role="alert" className="text-body-sm text-rust">
            {error}
          </p>
        )}
        <Button
          disabled={pending || !input || blocked}
          title={blocked ? '请先在模型广场接入模型 Provider' : undefined}
          onClick={submit}
          className="self-end bg-gradient-cta text-white hover:opacity-90"
        >
          {pending ? '发起中…' : '开始运行'}
        </Button>
      </div>
    </div>
  )
}
