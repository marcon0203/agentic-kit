import { useQuery } from '@tanstack/react-query'

import { apiClient, unwrap } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type ModelProvider = components['schemas']['ModelProvider']

/**
 * spec-15: every run entry point (workbench quick-create, Bundle list's
 * "运行", marketplace's "进入使用") must be disabled up front when no
 * Provider is connected yet, with a tooltip pointing at 模型中心 — never
 * "let them click and then show an error".
 */
export function useHasModelProvider() {
  const query = useQuery({
    queryKey: ['model-providers'],
    queryFn: async () => unwrap<ModelProvider[]>(await apiClient.GET('/model-providers', {})),
  })
  return { hasProvider: (query.data?.length ?? 0) > 0, isLoading: query.isLoading }
}
