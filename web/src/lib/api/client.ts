import createClient from 'openapi-fetch'

import type { paths, components } from '@/lib/api/schema'
import { useAuthStore } from '@/lib/auth/store'

type Envelope = components['schemas']['Envelope']

/**
 * Thrown for every non-zero `Envelope.code`, whatever the HTTP status —
 * spec-13: "封装统一信封解包...供上层按 code 分支处理（不匹配 message 文案）".
 * Callers should branch on `code`, never on `message` (Chinese copy can
 * change independently of the contract).
 */
export class ApiError extends Error {
  code: number
  details?: { field: string; reason: string }[]

  constructor(code: number, message: string, details?: { field: string; reason: string }[]) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.details = details
  }
}

export const apiClient = createClient<paths>({ baseUrl: '/api/v1' })

apiClient.use({
  onRequest({ request }) {
    const token = useAuthStore.getState().accessToken
    if (token) {
      request.headers.set('Authorization', `Bearer ${token}`)
    }
    return request
  },
  onResponse({ response }) {
    // No /auth/refresh endpoint exists yet in api/openapi.yaml (spec-04
    // issues a refresh_token but never defines an endpoint to redeem it) —
    // until that's added, a 401 degrades straight to "logged out", which
    // still satisfies spec-13's "刷新失败则登出" since there is nothing to
    // even attempt. Swap this for a real refresh-then-retry once the
    // endpoint exists.
    if (response.status === 401 && useAuthStore.getState().isAuthenticated()) {
      useAuthStore.getState().clearSession()
      useAuthStore.getState().openModal('session-expired')
    }
    return response
  },
})

/**
 * unwrap pulls the typed payload out of an openapi-fetch result — every
 * response (success or error) is an `Envelope`, so both `res.data` and
 * `res.error` land here; a non-zero `code` throws `ApiError`.
 */
export function unwrap<T>(res: { data?: Envelope; error?: Envelope }): T {
  const envelope = res.data ?? res.error
  if (!envelope) {
    throw new ApiError(-1, 'network error: no response received')
  }
  if (envelope.code !== 0) {
    throw new ApiError(envelope.code, envelope.message, envelope.details)
  }
  return envelope.data as T
}
