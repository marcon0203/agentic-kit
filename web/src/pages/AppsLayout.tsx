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
        { value: 'bundles', label: '应用管理', icon: Boxes },
        { value: 'agents', label: '智能体管理', icon: Bot },
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

// No description here on purpose — each section used to carry one, but a
// different length per item made the page jump on every switch. The title
// alone is enough; what each section does is obvious once you're in it.
const SECTION_TITLE: Record<Section, string> = {
  browse: '应用广场',
  bundles: '应用管理',
  agents: '智能体管理',
  tool: 'Tool',
  skill: 'Skill',
  mcp: 'MCP Server',
  knowledge_base: '知识库',
  memory: '记忆库',
  publish: '我的发布',
  subscriptions: '我的订阅',
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
  const segments = location.pathname.split('/apps/')[1]?.split('/') ?? []
  const section = (segments[0] || 'browse') as Section
  const title = SECTION_TITLE[section] ?? SECTION_TITLE.browse
  // Bundle create/edit (/apps/bundles/new, /apps/bundles/:ref/edit) is a
  // full-canvas tool, not a list — the section title row is dead weight
  // next to it. The sidebar still renders underneath so leaving the
  // editor is always one click, never a route dead-end.
  const isBundleEditor = section === 'bundles' && segments.length > 1

  return (
    <div className="flex flex-col gap-space-6">
      {!isBundleEditor && <PageHeader eyebrow="APPLICATIONS" title={title} />}

      <div className="flex flex-1 flex-col gap-space-6 sm:flex-row">
        <SectionSidebar groups={sectionGroups(knowledgeBaseEnabled)} active={section} onChange={(v) => navigate(`/apps/${v}`)} />

        <div className="min-w-0 flex-1">
          <Outlet />
        </div>
      </div>
    </div>
  )
}
