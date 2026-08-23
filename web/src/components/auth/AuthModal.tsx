import { useState } from 'react'
import * as Label from '@radix-ui/react-label'

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import { useAuthStore } from '@/lib/auth/store'

const REASON_COPY: Record<string, string> = {
  manual: '登录 / 注册',
  'protected-route': '登录后继续',
  'session-expired': '登录已过期，请重新登录',
}

export function AuthModal() {
  const modalOpen = useAuthStore((s) => s.modalOpen)
  const modalReason = useAuthStore((s) => s.modalReason)
  const closeModal = useAuthStore((s) => s.closeModal)
  const setSession = useAuthStore((s) => s.setSession)

  const [tab, setTab] = useState<'login' | 'register'>('login')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  function reset() {
    setEmail('')
    setPassword('')
    setDisplayName('')
    setError(null)
    setPending(false)
  }

  // Escape / close button / overlay click all route through onOpenChange,
  // and none of them touch auth state — design-system.md's 认证弹窗 spec.
  function handleOpenChange(open: boolean) {
    if (!open) {
      reset()
      closeModal()
    }
  }

  async function submit() {
    setPending(true)
    setError(null)
    try {
      if (tab === 'login') {
        const res = await apiClient.POST('/auth/login', { body: { email, password } })
        setSession(unwrap(res))
      } else {
        const res = await apiClient.POST('/auth/register', {
          body: { email, password, display_name: displayName },
        })
        setSession(unwrap(res))
      }
      reset()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '请求失败，请稍后重试')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open={modalOpen} onOpenChange={handleOpenChange}>
      <DialogContent
        className="w-[610px] max-w-[92vw] overflow-y-auto rounded-xl border-none p-8 text-white shadow-lg"
        style={{
          background:
            'radial-gradient(circle at 50% 0%, rgba(101,94,255,0.35), transparent 60%), linear-gradient(180deg, var(--color-navy-950), var(--color-navy-900))',
        }}
      >
        <DialogHeader>
          <DialogTitle className="text-title-modal text-white">
            {REASON_COPY[modalReason ?? 'manual']}
          </DialogTitle>
          <DialogDescription className="sr-only">
            {tab === 'login' ? '使用邮箱和密码登录' : '使用邮箱、密码和昵称注册新账号'}
          </DialogDescription>
        </DialogHeader>

        <Tabs value={tab} onValueChange={(v) => setTab(v as 'login' | 'register')} className="mt-space-4">
          <TabsList className="w-full bg-white/10">
            <TabsTrigger value="login" className="flex-1">
              登录
            </TabsTrigger>
            <TabsTrigger value="register" className="flex-1">
              注册
            </TabsTrigger>
          </TabsList>

          <TabsContent value="login" className="mt-space-5 flex flex-col gap-space-4">
            <Field label="邮箱" value={email} onChange={setEmail} type="email" autoComplete="email" />
            <Field
              label="密码"
              value={password}
              onChange={setPassword}
              type="password"
              autoComplete="current-password"
            />
          </TabsContent>

          <TabsContent value="register" className="mt-space-5 flex flex-col gap-space-4">
            <Field label="昵称" value={displayName} onChange={setDisplayName} autoComplete="nickname" />
            <Field label="邮箱" value={email} onChange={setEmail} type="email" autoComplete="email" />
            <Field
              label="密码"
              value={password}
              onChange={setPassword}
              type="password"
              autoComplete="new-password"
            />
          </TabsContent>
        </Tabs>

        {error && (
          <p role="alert" className="mt-space-3 text-body-sm text-[var(--color-error)]">
            {error}
          </p>
        )}

        <Button
          className="mt-space-6 w-full"
          size="lg"
          disabled={pending || !email || !password || (tab === 'register' && !displayName)}
          onClick={submit}
        >
          {pending ? '处理中…' : tab === 'login' ? '登录' : '注册'}
        </Button>
      </DialogContent>
    </Dialog>
  )
}

function Field({
  label,
  value,
  onChange,
  type = 'text',
  autoComplete,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  type?: string
  autoComplete?: string
}) {
  const id = `auth-field-${label}`
  return (
    <div className="flex flex-col gap-space-2">
      <Label.Root htmlFor={id} className="text-label-md text-white/80">
        {label}
      </Label.Root>
      <Input
        id={id}
        type={type}
        value={value}
        autoComplete={autoComplete}
        onChange={(e) => onChange(e.target.value)}
        className="h-12 rounded-sm border-white/20 bg-white/5 text-white placeholder:text-white/40"
      />
    </div>
  )
}
