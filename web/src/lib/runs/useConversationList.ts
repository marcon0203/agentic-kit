import { useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { Client } from 'openapi-fetch'

import { unwrap } from '@/lib/api/client'
import type { components, paths } from '@/lib/api/schema'

export type Conversation = components['schemas']['Conversation']

/**
 * /chat/bundle/:bundleId 左侧"最近对话"列表的数据源。走独立的 client 参
 * 数（平台的 apiClient 或访客的 guestClient）而不是自己二选一——两套鉴
 * 权体系互不感知，这个 hook 不该替调用方做这个决定。
 */
export function useConversationList(client: Client<paths>, bundleRef: string | undefined, ready: boolean) {
  const queryClient = useQueryClient()
  const queryKey = ['bundle-conversations', bundleRef]

  const query = useQuery({
    queryKey,
    queryFn: async () =>
      unwrap<Conversation[]>(
        await client.GET('/bundles/{ref}/conversations', { params: { path: { ref: bundleRef! } } }),
      ),
    enabled: ready && !!bundleRef,
  })

  const refresh = useCallback(() => {
    queryClient.invalidateQueries({ queryKey })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bundleRef])

  return { conversations: query.data ?? [], isLoading: query.isLoading, refresh }
}
