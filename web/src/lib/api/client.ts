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
 * JSON response (success or error) is an `Envelope`, so both `res.data`
 * and `res.error` land here; a non-zero `code` throws `ApiError`. Use
 * this for any 200/201 endpoint with a body.
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

/**
 * assertOk is unwrap's counterpart for the handful of endpoints that
 * return 204 No Content on success (cancelRun, resolveGate, deleteBundle,
 * deleteAgent, ...) — there's no envelope to pull data out of, but a
 * failure still comes back as one via `res.error`, and openapi-fetch
 * never throws on its own, so skipping this check silently treats every
 * error response as a success. Every call site MUST route through this
 * or `unwrap` — never leave a bare `await apiClient.POST/PATCH/DELETE`.
 */
export function assertOk(res: { error?: Envelope }): void {
  if (res.error) {
    throw new ApiError(res.error.code, res.error.message, res.error.details)
  }
}
