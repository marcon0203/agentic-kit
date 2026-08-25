import { Blocks, Braces, Puzzle, Terminal } from 'lucide-react'

/**
 * 组件的两个展示维度，都存在资源的 config JSONB 里，没有新增数据库列
 * （spec-05a §4/§7 的既定做法）：
 *
 * - `config.category`：使用场景，组件广场第二行筛选用的分类。纯展示/检索
 *   维度，运行时完全不读它。
 * - `config.component_type` + `config.tool_type`：组件到底是什么，运行时
 *   真正按它分流（见 internal/orchestrator/adk 的 compileTools /
 *   buildEndpointTool）。这里只是把它翻译成一句人话摆在卡片上。
 */

/** 使用场景。存进 config 的是英文 key，界面文案改了也不会让旧数据错位。 */
export const COMPONENT_CATEGORIES = [
  { value: 'business', label: '商业服务' },
  { value: 'lifestyle', label: '生活方式' },
  { value: 'media', label: '图像视频' },
  { value: 'productivity', label: '效率工具' },
  { value: 'education', label: '学习教育' },
] as const

export type ComponentCategory = (typeof COMPONENT_CATEGORIES)[number]['value']

/** 没填分类的组件（含本次改动之前注册的存量数据）统一落到"未分类"。 */
export const UNCATEGORIZED = '未分类'

export function categoryLabel(value: string | undefined): string {
  return COMPONENT_CATEGORIES.find((c) => c.value === value)?.label ?? UNCATEGORIZED
}

/** 组件形态：component_type 与 tool_type 两层判别压平成一个展示用的枚举。 */
export type ComponentShape = 'http' | 'openapi' | 'sandbox' | 'plugin'

export const COMPONENT_SHAPE_META: Record<ComponentShape, { label: string; icon: typeof Blocks }> = {
  http: { label: 'HTTP 接口', icon: Blocks },
  openapi: { label: 'OpenAPI 导入', icon: Braces },
  sandbox: { label: '沙箱环境', icon: Terminal },
  plugin: { label: '插件', icon: Puzzle },
}

/** 一个组件资源的 config 里这几个字段是广场页面会读的。 */
export interface ComponentConfig {
  category?: string
  component_type?: string
  tool_type?: string
  description?: string
  endpoint?: string
  method?: string
  path?: string
}

export function componentConfig(config: unknown): ComponentConfig {
  return (config ?? {}) as ComponentConfig
}

/**
 * 缺 component_type / tool_type 的按 http 处理——本次判别字段落地之前注册
 * 的组件就是这个形态，运行时也是这么兜底的，展示层跟着保持一致。
 */
export function componentShape(config: ComponentConfig): ComponentShape {
  if (config.component_type === 'sandbox') return 'sandbox'
  if (config.component_type === 'plugin') return 'plugin'
  return config.tool_type === 'openapi' ? 'openapi' : 'http'
}

/**
 * 卡片上那两行描述。用户自己填了说明就用他的，没填就拿 config 里已有的
 * 信息拼一句——总比一张卡片上空着一块强。
 */
export function componentDescription(config: ComponentConfig): string {
  if (config.description) return config.description
  switch (componentShape(config)) {
    case 'openapi':
      return [config.method, config.path].filter(Boolean).join(' ') || '从 OpenAPI spec 导入的接口。'
    case 'sandbox':
      return 'Daytona 沙箱：执行代码与 shell 命令。'
    case 'plugin':
      return '插件组件。'
    default:
      return config.endpoint || '未填写说明。'
  }
}
