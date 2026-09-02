import { useQuery } from '@tanstack/react-query'

import { apiClient, unwrap } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ModelAccess = components['schemas']['ModelAccess']

/**
 * spec-15: every run entry point (workbench quick-create, Bundle list's
 * "运行", marketplace's "进入使用") must be disabled up front when no
 * Provider is connected yet, with a tooltip pointing at 模型广场 — never
 * "let them click and then show an error".
 *
 * Reads GET /me/model-access, not GET /model-providers: the latter lists
 * only what this account connected itself, while a run also works off an
 * admin's org-wide credential (系统配置 → 模型提供商). Gating on the
 * personal list alone greyed out every 运行 button for accounts that run
 * purely on the org default, even though those runs would have succeeded —
 * this endpoint answers with the same set the run pre-flight uses.
 */
export function useHasModelProvider() {
  const query = useQuery({
    queryKey: ['model-access'],
    queryFn: async () => unwrap<ModelAccess>(await apiClient.GET('/me/model-access', {})),
  })
  return { hasProvider: query.data?.has_provider ?? false, isLoading: query.isLoading }
}
