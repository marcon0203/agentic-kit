import { useMemo } from 'react'
import {
  AssistantRuntimeProvider,
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  useExternalStoreRuntime,
  type AppendMessage,
  type DataMessagePartProps,
  type TextMessagePartProps,
  type ThreadMessageLike,
} from '@assistant-ui/react'
import { ArrowDown, Send, Square } from 'lucide-react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

import { GateCard } from '@/components/run/GateCard'
import { PluginRenderCard } from '@/components/run/PluginRenderCard'
import type { GateEntry, RenderEntry } from '@/lib/runs/threadMessages'
import { cn } from '@/lib/utils'

export interface AgentThreadProps {
  messages: ThreadMessageLike[]
  isRunning: boolean
  onSend: (text: string) => Promise<void>
  onCancel?: () => Promise<void> | void
  /** 配置不完整之类的原因，整条线程连输入都禁掉。 */
  disabled?: boolean
  disabledHint?: string
  /** 空对话时的引导文案。 */
  emptyTitle?: string
  emptyHint?: string
  /** 人工审批：谁能批、批了怎么办。不传就只展示状态。 */
  gate?: {
    canApprove: boolean
    onResolve: (node: string, approved: boolean) => Promise<void>
  }
  className?: string
  /** 顶栏，留给调用方塞标题、状态、操作。 */
  header?: React.ReactNode
  /** 输入框下方的一行说明。 */
  footerNote?: React.ReactNode
}

/**
 * 对话界面，建在 assistant-ui 的 primitives 上。
 *
 * 用 useExternalStoreRuntime 而不是它自带的传输层：数据源仍然是现成的
 * NDJSON 事件流 + buildTimeline（人工审批、插件渲染这些东西都在里面），
 * assistant-ui 只接管渲染与交互——流式打字、markdown、自动滚动与"回到底
 * 部"、多行输入、复制、编辑重发。这些之前都是手写的，效果和它差得远。
 *
 * 消息数组由调用方（useConversation）给，这里不持有对话状态。
 */
export function AgentThread({
  messages,
  isRunning,
  onSend,
  onCancel,
  disabled,
  disabledHint,
  emptyTitle,
  emptyHint,
  gate,
  className,
  header,
  footerNote,
}: AgentThreadProps) {
  const runtime = useExternalStoreRuntime({
    messages,
    isRunning,
    isDisabled: disabled,
    // convertMessage 是恒等的：threadMessages.ts 已经产出 ThreadMessageLike，
    // 转换逻辑集中在那一处，比散在这里更好找。
    convertMessage: (m: ThreadMessageLike) => m,
    onNew: async (message: AppendMessage) => {
      const text = message.content
        .map((p) => (p.type === 'text' ? p.text : ''))
        .join('')
        .trim()
      if (text) await onSend(text)
    },
    onCancel: onCancel ? async () => void (await onCancel()) : undefined,
  })

  // gate 回调会随父组件每次渲染换引用，包一层免得审批卡整棵重挂。
  const parts = useMemo(() => makeParts(gate), [gate])

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ThreadPrimitive.Root className={cn('flex min-h-0 flex-1 flex-col bg-surface', className)}>
        {header}

        <ThreadPrimitive.Viewport
          autoScroll
          className="relative flex min-h-0 flex-1 flex-col gap-space-5 overflow-y-auto px-space-5 py-space-5"
        >
          <ThreadPrimitive.Empty>
            <div className="flex flex-1 flex-col items-center justify-center gap-space-2 text-center">
              <span className="text-label-md text-ink-900">{emptyTitle ?? '开始对话'}</span>
              <span className="text-body-sm text-ink-500">{emptyHint ?? '输入问题试试'}</span>
            </div>
          </ThreadPrimitive.Empty>

          <ThreadPrimitive.Messages components={{ UserMessage, AssistantMessage: assistantMessageWith(parts) }} />

          {/* 用户往上翻之后不再自动跟随，改成一个"回到底部"的按钮——被拽回
              去比看不到新消息更烦人。 */}
          <ThreadPrimitive.ScrollToBottom asChild>
            <button
              type="button"
              className="text-body-sm sticky bottom-space-2 z-10 mx-auto flex items-center gap-space-2 rounded-full border border-border bg-surface px-space-4 py-space-2 shadow-sm hover:border-border-strong disabled:hidden"
            >
              <ArrowDown className="size-4" aria-hidden />
              回到底部
            </button>
          </ThreadPrimitive.ScrollToBottom>
        </ThreadPrimitive.Viewport>

        {disabled && disabledHint && (
          <p className="text-caption border-t border-border px-space-5 py-space-2 text-ink-500">{disabledHint}</p>
        )}

        <ComposerPrimitive.Root className="flex shrink-0 items-end gap-space-2 border-t border-border p-space-3">
          <ComposerPrimitive.Input
            rows={2}
            autoFocus
            placeholder={disabled ? (disabledHint ?? '暂时不能发送') : '输入消息，Enter 发送，Shift+Enter 换行'}
            aria-label="消息输入"
            className="text-body-md max-h-40 min-h-[3.25rem] flex-1 resize-none rounded-md border border-border bg-surface px-space-3 py-space-2 text-ink-900 outline-none placeholder:text-ink-500 focus-visible:border-border-strong disabled:cursor-not-allowed disabled:opacity-60"
          />
          <ThreadPrimitive.If running={false}>
            <ComposerPrimitive.Send asChild>
              <button
                type="button"
                aria-label="发送"
                className="flex size-9 shrink-0 items-center justify-center rounded-md bg-primary text-primary-foreground transition-opacity hover:opacity-90 disabled:opacity-40"
              >
                <Send className="size-4" aria-hidden />
              </button>
            </ComposerPrimitive.Send>
          </ThreadPrimitive.If>
          <ThreadPrimitive.If running>
            <ComposerPrimitive.Cancel asChild>
              <button
                type="button"
                aria-label="停止"
                className="flex size-9 shrink-0 items-center justify-center rounded-md border border-border text-ink-700 hover:border-border-strong"
              >
                <Square className="size-3.5 fill-current" aria-hidden />
              </button>
            </ComposerPrimitive.Cancel>
          </ThreadPrimitive.If>
        </ComposerPrimitive.Root>

        {footerNote && <div className="shrink-0 border-t border-border px-space-5 py-space-2">{footerNote}</div>}
      </ThreadPrimitive.Root>
    </AssistantRuntimeProvider>
  )
}

// ── 消息 ──────────────────────────────────────────────────────────────

function UserMessage() {
  return (
    <MessagePrimitive.Root className="flex justify-end">
      <div className="text-body-md max-w-[78%] rounded-lg bg-blueprint-tint px-space-4 py-space-3 whitespace-pre-wrap text-ink-900">
        <MessagePrimitive.Parts />
      </div>
    </MessagePrimitive.Root>
  )
}

function assistantMessageWith(parts: ReturnType<typeof makeParts>) {
  return function AssistantMessage() {
    return (
      <MessagePrimitive.Root className="flex flex-col gap-space-3">
        <div className="text-body-md max-w-full text-ink-900">
          <MessagePrimitive.Parts components={parts} />
        </div>
        <MessagePrimitive.Error>
          <p className="text-body-sm rounded-md bg-rust-tint px-space-3 py-space-2 text-rust">这一轮没能跑完</p>
        </MessagePrimitive.Error>
      </MessagePrimitive.Root>
    )
  }
}

function makeParts(gate: AgentThreadProps['gate']) {
  return {
    Text: MarkdownText,
    data: { Fallback: dataPartWith(gate) },
  }
}

/**
 * 模型输出按 markdown 渲染。之前是 whitespace-pre-wrap 的纯文本，模型写
 * 的标题、列表、代码块全是一坨原始符号。
 */
function MarkdownText({ text }: TextMessagePartProps) {
  return (
    <div className="prose prose-sm dark:prose-invert max-w-none break-words text-ink-900 prose-pre:bg-surface-muted prose-pre:text-ink-900 prose-code:text-ink-900">
      <Markdown remarkPlugins={[remarkGfm]}>{text}</Markdown>
    </div>
  )
}

/**
 * 自定义 part 的分派。
 *
 * threadMessages.ts 里写的是 `data-plugin-render` 这样的类型名，
 * assistant-ui 把它规范化成 `{ type: 'data', name: 'plugin-render' }`，
 * 所以这里按 name 分派。用 Fallback 而不是 `data.by_name`：一处 switch
 * 比一张映射表更容易跟着上面那份产出走。
 */
function dataPartWith(gate: AgentThreadProps['gate']) {
  return function DataPart(props: DataMessagePartProps) {
    const { name, data } = props
    if (name === 'plugin-render') {
      return <PluginRenderCard entry={data as RenderEntry} />
    }
    if (name === 'gate') {
      const entry = data as GateEntry
      return (
        <GateCard
          gate={entry}
          canApprove={gate?.canApprove ?? false}
          onResolve={gate?.onResolve ?? (async () => {})}
        />
      )
    }
    if (name === 'run-error') {
      const { note } = data as { note?: string }
      return <p className="text-body-sm rounded-md bg-rust-tint px-space-3 py-space-2 text-rust">{note ?? '运行失败'}</p>
    }
    return null
  }
}
