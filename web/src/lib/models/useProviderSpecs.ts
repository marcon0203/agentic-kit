import { useQuery } from '@tanstack/react-query'

import { apiClient, unwrap } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

export type ProviderSpec = components['schemas']['ModelProviderSpec']

/**
 * 平台支持的模型渠道。**唯一来源是后端**：渠道由管理员在
 * 系统配置 → 模型提供商 里从协议模板创建，前端不能也不该猜有哪些。
 *
 * 所以这里没有兜底列表——加载不出来就是空的。给一份假的兜底列表，只会让用
 * 户在下拉框里选到一个这个部署根本没有的渠道，然后在运行时收获一句"no
 * client configured"。
 */
export function useProviderSpecs() {
  const query = useQuery({
    queryKey: ['model-provider-specs'],
    queryFn: async () =>
      unwrap<{ items: ProviderSpec[] }>(await apiClient.GET('/model-provider-specs', {})),
    staleTime: 60_000,
  })
  return {
    specs: query.data?.items ?? [],
    isLoading: query.isLoading,
    labelOf: (name: string) => query.data?.items?.find((s) => s.name === name)?.label ?? name,
  }
}
