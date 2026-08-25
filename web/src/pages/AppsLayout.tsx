import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Store, Boxes, Bot, Wrench, Puzzle, Plug, BookOpen, Brain, Upload, Heart } from 'lucide-react'

import { PageHeader } from '@/components/common/Page'
import { SectionSidebar, type SectionSidebarGroup } from '@/components/layout/SectionSidebar'
import { useFeatures } from '@/lib/features/useFeatures'

type Section =
  | 'browse'
  | 'bundles'
  | 'agents'
  | 'tool'
  | 'skill'
  | 'mcp'
  | 'knowledge_base'
  | 'memory'
  | 'publish'
  | 'subscriptions'

function sectionGroups(knowledgeBaseEnabled: boolean): SectionSidebarGroup[] {
  return [
    { items: [{ value: 'browse', label: '应用广场', icon: Store }] },
    {
      label: '应用',
      items: [
        { value: 'bundles', label: 'Bundle 编排', icon: Boxes },
        { value: 'agents', label: 'Agent 定义', icon: Bot },
      ],
    },
    {
      label: '资源',
      items: [
        { value: 'tool', label: 'Tool', icon: Wrench },
        { value: 'skill', label: 'Skill', icon: Puzzle },
        { value: 'mcp', label: 'MCP Server', icon: Plug },
        // 知识库依赖 Milvus + Elasticsearch（多路召回）；未部署时
        // GET /features 报 knowledge_base_enabled=false，隐藏这一项而不是
        // 留一个点了就报错的入口。
        ...(knowledgeBaseEnabled ? [{ value: 'knowledge_base', label: '知识库', icon: BookOpen }] : []),
        { value: 'memory', label: '记忆库', icon: Brain },
      ],
    },
    {
      label: '我的',
      items: [
        { value: 'publish', label: '我的发布', icon: Upload },
        { value: 'subscriptions', label: '我的订阅', icon: Heart },
      ],
    },
  ]
}

const SECTION_COPY: Record<Section, { title: string; description: string }> = {
  browse: {
    title: '应用广场',
    description: '所有人发布的 Bundle、Agent、Skill 与 MCP Server。订阅锁定版本，作者的编排图和提示词不会跟着过来。',
  },
  bundles: {
    title: 'Bundle 编排',
    description: '把多个 Agent 编排成一次协作：谁先做、谁并行、哪一步必须有人点头。运行从这里发起。',
  },
  agents: {
    title: 'Agent 定义',
    description: '单个角色的定义：模型、人设、能力白名单与执行约束，按版本管理。',
  },
  tool: {
    title: 'Tool',
    description: 'Agent 能调用的外部能力：一个检索接口、一个内部服务。注册后才能写进 Agent 的能力白名单。',
  },
  skill: {
    title: 'Skill',
    description: '把一段固定的做事方式打包，让多个 Agent 共用同一套步骤，而不是各写各的提示词。',
  },
  mcp: {
    title: 'MCP Server',
    description: '登记地址与凭证后平台会立刻探测一次连通性。凭证加密落库，任何响应都不会带出来。',
  },
  knowledge_base: {
    title: '知识库',
    description: '登记后可以被 Agent 引用，回答时从这里做向量检索，而不是全靠模型自己记得。',
  },
  memory: {
    title: '记忆库',
    description: '同一个账号下的运行会把对话写进这里；Agent 勾选 load_memory / preload_memory 内置工具即可检索，重启进程也不会丢。',
  },
  publish: {
    title: '我的发布',
    description: '把自己的 Bundle 或 Agent 发布到广场，让其他人可以订阅使用；广场上看到的仍然是黑盒，编排图与提示词不会带出去。',
  },
  subscriptions: {
    title: '我的订阅',
    description: '订阅版本已锁定，作者发布新版本时会单独提醒，是否升级由你决定。',
  },
}

/**
 * 应用中心的外壳：顶部横向导航是一级菜单，这里的左侧栏是二级菜单，每一项都
 * 是 /apps/<section> 下的独立路由，而不是同一个页面里靠 tab 切换——路由各管
 * 各的，这个壳只负责侧栏 + 页头，具体内容交给各自的路由页面渲染。
 */
export function AppsLayout() {
  const location = useLocation()
  const navigate = useNavigate()
  const { knowledgeBaseEnabled } = useFeatures()
  const section = (location.pathname.split('/apps/')[1]?.split('/')[0] || 'browse') as Section
  const copy = SECTION_COPY[section] ?? SECTION_COPY.browse

  return (
    <div className="flex flex-col gap-space-6">
      <PageHeader eyebrow="APPLICATIONS" title={copy.title} description={copy.description} />

      <div className="flex flex-col gap-space-6 sm:flex-row">
        <SectionSidebar groups={sectionGroups(knowledgeBaseEnabled)} active={section} onChange={(v) => navigate(`/apps/${v}`)} />

        <div className="min-w-0 flex-1">
          <Outlet />
        </div>
      </div>
    </div>
  )
}
