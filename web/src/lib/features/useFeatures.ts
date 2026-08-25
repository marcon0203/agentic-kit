import { useQuery } from '@tanstack/react-query'

import { apiClient, unwrap } from '@/lib/api/client'
import { useAuthStore } from '@/lib/auth/store'

/**
 * GET /features mirrors config.Config.KBEnabled (and any future server-side
 * switch) — 知识库 depends on Milvus + Elasticsearch actually being
 * deployed, so a fresh install with neither one running hides the sidebar
 * entry instead of offering a resource kind every create/ingest call would
 * reject.
 */
export function useFeatures() {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated())
  const query = useQuery({
    queryKey: ['features'],
    queryFn: async () =>
      unwrap<{ knowledge_base_enabled: boolean }>(await apiClient.GET('/features', {})),
    enabled: isAuthenticated,
  })
  return {
    // Default true while loading: a resource-kind page briefly visible
    // before the first response lands is a smaller cost than every
    // resource-kind page flashing "disabled" on every load.
    knowledgeBaseEnabled: query.data?.knowledge_base_enabled ?? true,
    isLoading: query.isLoading,
  }
}
