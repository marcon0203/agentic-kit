import deepseekIcon from '@lobehub/icons-static-svg/icons/deepseek-color.svg'
import volcengineIcon from '@lobehub/icons-static-svg/icons/volcengine-color.svg'
import qwenIcon from '@lobehub/icons-static-svg/icons/qwen-color.svg'
import openaiIcon from '@lobehub/icons-static-svg/icons/openai.svg'

import { cn } from '@/lib/utils'

/**
 * 供应商图标。
 *
 * 按**协议模板**取图标而不是按 provider key：key 是管理员随便起的（"我的
 * DeepSeek"、"deepseek-hk"），模板才对应一个确定的厂商。
 *
 * 用 @lobehub/icons-static-svg 而不是 @lobehub/icons：后者是 antd + React
 * 组件包，会把 antd 6 和 @lobehub/ui 拖进这个 Tailwind + shadcn 的项目；
 * 前者是同一套图标的纯 SVG 资源，零依赖。
 */
const TEMPLATE_ICON: Record<string, string> = {
  deepseek: deepseekIcon,
  volcengine: volcengineIcon,
  qwen: qwenIcon,
  'openai-compatible': openaiIcon,
}

export function providerIconFor(template: string | undefined | null): string | undefined {
  return template ? TEMPLATE_ICON[template] : undefined
}

export function ProviderIcon({
  template,
  icon,
  name,
  className,
}: {
  /** 协议模板 id，决定用哪个厂商图标 */
  template?: string | null
  /** 管理员自己上传的图标（URL 或 data: URI），优先于模板图标 */
  icon?: string | null
  /** 两者都没有时用首字母兜底 */
  name: string
  className?: string
}) {
  const src = icon || providerIconFor(template)
  if (src) {
    return <img src={src} alt="" className={cn('size-8 shrink-0 rounded-sm object-contain', className)} />
  }
  return (
    <span
      aria-hidden
      className={cn(
        'text-caption flex size-8 shrink-0 items-center justify-center rounded-sm bg-surface-muted font-medium text-ink-500',
        className,
      )}
    >
      {name.slice(0, 1).toUpperCase()}
    </span>
  )
}
