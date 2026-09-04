import { Link } from 'react-router-dom'

import { Button } from '@/components/ui/button'

/**
 * 一个 Bundle 发布成功之后，能被调用的方式有两种：平台自带的标准会话页面
 * （订阅之后在"应用广场"里点开就是），和这里说的 Open API——第三方自己的
 * 系统直接调 POST /runs，不经过这个前端。作者管理自己发布的列表（我的发
 * 布）和订阅者看资源详情页，看到的是同一块内容。
 *
 * curl 示例里的 bundle_ref 就是这条 listing 的 listing_ref：作者自己调用
 * 时它按所有权直接解析，其他人调用时按订阅解析（run.BundleResolver），
 * 同一个值两条路都通，不用在这里分情况写两段示例。
 */
export function ApiUsagePanel({ listingRef }: { listingRef: string }) {
  const curl = [
    `curl -X POST ${window.location.origin}/api/v1/runs \\`,
    `  -H "Authorization: ApiKey <你的 API Key>" \\`,
    `  -H "Content-Type: application/json" \\`,
    `  -H "Idempotency-Key: $(uuidgen)" \\`,
    `  -d '{"bundle_ref": "${listingRef}", "input": {"message": "你好"}}'`,
  ].join('\n')

  return (
    <div className="flex flex-col gap-space-3">
      <p className="text-body-sm text-ink-700">
        第三方系统直接调 Open API——鉴权、事件流读取都和平台自己用的是同一套接口，不是单独开的口子。
      </p>
      <pre className="text-ref-sm overflow-x-auto rounded-md bg-ink-900 px-space-4 py-space-3 text-white">
        <code>{curl}</code>
      </pre>
      <p className="text-body-sm text-ink-500">
        响应里的 <code className="text-ref-sm">run_id</code> 再拿去调
        <code className="text-ref-sm mx-1">GET /runs/&#123;id&#125;/stream</code>
        读事件流，或轮询 <code className="text-ref-sm">GET /runs/&#123;id&#125;</code> 拿最终状态。调用方需要先订阅这个应用（作者本人除外）。
      </p>
      <div className="flex items-center gap-space-4">
        <Button asChild variant="outline" size="sm">
          <Link to="/settings/api-keys" target="_blank" rel="noopener noreferrer">
            去生成 API Key
          </Link>
        </Button>
        <a
          href="/openapi.yaml"
          target="_blank"
          rel="noopener noreferrer"
          className="text-body-sm text-blueprint hover:underline"
        >
          完整接口文档（OpenAPI）
        </a>
      </div>
    </div>
  )
}
