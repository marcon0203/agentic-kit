import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useAuthStore } from '@/lib/auth/store'

export function HomePage() {
  const user = useAuthStore((s) => s.user)

  return (
    <div className="flex flex-col gap-space-7">
      <div>
        <p className="text-eyebrow text-primary">AI Agent 平台</p>
        <h1 className="text-headline-lg mt-space-2 text-ink-900">
          {user ? `欢迎回来，${user.display_name}` : '编排、运行、审批你的 Agent Bundle'}
        </h1>
        <p className="text-body-lg mt-space-3 max-w-[720px] text-ink-700">
          从应用广场订阅一个 Bundle，或在应用中心创建自己的编排——Chat
          式运行详情页、human gate 审批与运营看板由后续任务陆续接入。
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-headline-sm">脚手架状态</CardTitle>
        </CardHeader>
        <CardContent className="text-body-md text-ink-700">
          导航、路由、认证弹窗与 API client（由 openapi.yaml 生成类型）已就绪——spec-13
          的基础设施到此完成，Chat 运行详情页等真实页面见 spec-14 起。
        </CardContent>
      </Card>
    </div>
  )
}
