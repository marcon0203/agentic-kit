import createClient from 'openapi-fetch'

import type { paths } from '@/lib/api/schema'

/**
 * 匿名"立即体验"页专用的客户端——和平台自己那套（apiClient + useAuthStore）
 * 完全独立，两者互不感知。这是产品要求本身：访客的鉴权体系必须独立于平台，
 * 不能因为同一个浏览器里恰好登录着一个平台账号，就把平台的 token 带进这个
 * 公开页面的请求里（反过来也一样——访客的临时身份不该泄漏进平台会话）。
 *
 * 存在 sessionStorage 而不是 localStorage：访客身份是这一次"体验"用的，
 * 关掉标签页就该结束，不需要跨会话持久化，也不需要能在多个标签页之间
 * 共享（每个标签页各自的一次性身份，互不干扰）。
 */
const STORAGE_KEY = 'agentic-kit-guest-session'

function readStoredToken(): string | null {
  try {
    return sessionStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

let guestToken: string | null = readStoredToken()

export function getGuestAccessToken(): string | null {
  return guestToken
}

export function setGuestAccessToken(token: string): void {
  guestToken = token
  try {
    sessionStorage.setItem(STORAGE_KEY, token)
  } catch {
    // sessionStorage 不可用（隐私模式之类）时退化为纯内存——这个标签页
    // 刷新一次就得重新拿一个新访客身份，但至少这一次会话内还能用。
  }
}

export const guestClient = createClient<paths>({ baseUrl: '/api/v1' })

guestClient.use({
  onRequest({ request }) {
    if (guestToken) {
      request.headers.set('Authorization', `Bearer ${guestToken}`)
    }
    return request
  },
})
