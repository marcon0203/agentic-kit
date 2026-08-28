import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Store, Boxes, Bot, Wrench, Puzzle, Plug, BookOpen, Brain, Upload, Heart } from 'lucide-react'

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
        { value: 'bundles', label: '应用管理', icon: Boxes },
        { value: 'agents', label: '智能体管理', icon: Bot },
      ],
    },
    {
      label: '资源',
      items: [
        { value: 'tool', label: '组件', icon: Wrench },
        { value: 'skill', label: 'Skill 管理', icon: Puzzle },
        { value: 'mcp', label: 'MCP 管理', icon: Plug },
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

/**
 * 应用中心的外壳：顶部横向导航是一级菜单，这里的左侧栏是二级菜单，每一项都
 * 是 /apps/<section> 下的独立路由，而不是同一个页面里靠 tab 切换——路由各管
 * 各的，这个壳只负责侧栏，具体内容交给各自的路由页面渲染。侧栏已高亮当前
 * 分区，页面不再重复渲染分区名页头。
 */
export function AppsLayout() {
  const location = useLocation()
  const navigate = useNavigate()
  const { knowledgeBaseEnabled } = useFeatures()
  const section = ((location.pathname.split('/apps/')[1] ?? '').split('/')[0] || 'browse') as Section

  return (
    <div className="flex flex-1 flex-col gap-space-6 sm:flex-row">
      <SectionSidebar groups={sectionGroups(knowledgeBaseEnabled)} active={section} onChange={(v) => navigate(`/apps/${v}`)} />

      {/* 内容区做成一张浮在页面底色上的卡片；侧栏裸露在卡片外，无边框。 */}
      <div className="min-w-0 flex-1 rounded-lg border border-border bg-surface p-space-6">
        <Outlet />
      </div>
    </div>
  )
}
