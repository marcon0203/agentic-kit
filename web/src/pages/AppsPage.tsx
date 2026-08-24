import { useSearchParams } from 'react-router-dom'
import { Store, Boxes, Bot, Wrench, Puzzle, Plug, BookOpen, Brain, Upload, Heart } from 'lucide-react'

import { PageHeader } from '@/components/common/Page'
import { SectionSidebar, type SectionSidebarGroup } from '@/components/layout/SectionSidebar'
import { MarketplaceBrowsePage } from '@/pages/MarketplaceBrowsePage'
import { BundleListPage } from '@/pages/BundleListPage'
import { AgentDefinitionPage } from '@/pages/AgentDefinitionPage'
import { MyListingsPage } from '@/pages/MyListingsPage'
import { MySubscriptionsPage } from '@/pages/MySubscriptionsPage'
import { ResourceKindPage } from '@/pages/ResourceCenterPage'
import { useAuthStore } from '@/lib/auth/store'
import type { components } from '@/lib/api/schema'

type ResourceType = components['schemas']['ResourceType']

type Section = 'browse' | 'bundles' | 'agents' | 'tool' | 'skill' | 'mcp' | 'knowledge_base' | 'memory' | 'publish' | 'subscriptions'

const SECTION_GROUPS: SectionSidebarGroup[] = [
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
      { value: 'knowledge_base', label: '知识库', icon: BookOpen },
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

const RESOURCE_SECTIONS = new Set<Section>(['tool', 'skill', 'mcp', 'knowledge_base', 'memory'])

/**
 * 应用广场（发布市场）、Bundle/Agent 编排与资源登记（Tool/Skill/MCP/知识库/
 * 记忆库）合并成一个中心：顶部横向导航是一级菜单，这里的左侧栏是二级菜单，
 * 每一项都是独立页面而不是同一个页面里的 Tab——Bundle 和 Agent 配置项差别
 * 很大，MCP 有连通性探测、知识库有向量模型配置，揉在一起切换只会让人以为
 * 它们是同一件事的不同视图。
 */
export function AppsPage() {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated())
  const [searchParams, setSearchParams] = useSearchParams()
  const section = ((searchParams.get('tab') as Section) || 'browse') as Section

  function setSection(next: Section) {
    setSearchParams(next === 'browse' ? {} : { tab: next })
  }

  const copy = SECTION_COPY[section]

  return (
    <div className="flex flex-col gap-space-6">
      <PageHeader eyebrow="APPLICATIONS" title={copy.title} description={copy.description} />

      <div className="flex flex-col gap-space-6 sm:flex-row">
        <SectionSidebar groups={SECTION_GROUPS} active={section} onChange={(v) => setSection(v as Section)} />

        <div className="min-w-0 flex-1">
          {section === 'browse' && <MarketplaceBrowsePage />}
          {section === 'bundles' && <BundleListPage />}
          {section === 'agents' && <AgentDefinitionPage />}
          {RESOURCE_SECTIONS.has(section) && <ResourceKindPage type={section as ResourceType} />}
          {section === 'publish' && isAuthenticated && <MyListingsPage />}
          {section === 'subscriptions' && isAuthenticated && <MySubscriptionsPage />}
        </div>
      </div>
    </div>
  )
}
