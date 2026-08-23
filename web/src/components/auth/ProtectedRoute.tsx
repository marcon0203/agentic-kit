import { useEffect } from 'react'
import type { ReactNode } from 'react'

import { useAuthStore } from '@/lib/auth/store'

/**
 * spec-13: "未登录访问受保护路由自动跳转登录页" — but design-system.md's
 * own correction says auth is never a route ("登录注册改为弹窗，删除独立
 * 路由"), so the guard opens the auth modal in place rather than
 * navigating anywhere; the URL never shows a /login path.
 */
export function ProtectedRoute({ children }: { children: ReactNode }) {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated())
  const openModal = useAuthStore((s) => s.openModal)

  useEffect(() => {
    if (!isAuthenticated) {
      openModal('protected-route')
    }
  }, [isAuthenticated, openModal])

  if (!isAuthenticated) {
    return null
  }
  return <>{children}</>
}
