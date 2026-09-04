import { useEffect, useState } from 'react'

import { guestClient, getGuestAccessToken, setGuestAccessToken } from '@/lib/guest/guestClient'
import { unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type AuthResult = components['schemas']['AuthResult']

/**
 * 保证这个标签页有一个访客身份可用——有存下来的 token 就直接用，没有就
 * 现拿一个（POST /public/guest-sessions，见 iam.Service.CreateGuest）。
 * 访客页的其它所有请求都得等这个 ready 之后才能发，不然请求上没有
 * Authorization 头，会被当成真正匿名（未鉴权）的调用直接 401。
 */
export function useGuestSession() {
  const [ready, setReady] = useState(!!getGuestAccessToken())
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (getGuestAccessToken()) {
      setReady(true)
      return
    }
    let cancelled = false
    ;(async () => {
      try {
        const session = unwrap<AuthResult>(await guestClient.POST('/public/guest-sessions', {}))
        if (cancelled) return
        setGuestAccessToken(session.access_token)
        setReady(true)
      } catch (err) {
        if (cancelled) return
        setError(err instanceof ApiError ? err.message : '初始化失败，请刷新页面重试')
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  return { ready, error }
}
