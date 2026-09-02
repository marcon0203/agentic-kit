import { Play, Trash2 } from 'lucide-react'

import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Checkbox } from '@/components/ui/checkbox'
import { Button } from '@/components/ui/button'
import { validateCondition } from '@/lib/bundleEditor/validateCondition'
import type { AgentNode, BundleEdge, BundleRunType } from '@/lib/bundleEditor/graphIO'

const RUN_TYPE_HELP: Record<BundleRunType, string> = {
  graph: '完整编排图：支持条件分支、并行 fan-out、join 汇合与自循环重试。',
  flow: '顺序流程：agents 按加入画布的顺序严格串行执行一次，无分支/并行。',
  single: '单体：只运行一个 agent，没有编排开销。',
}

interface ValidationIssue {
  target: string
  reason: string
}

export function PropertiesPanel({
  selectedNode,
  selectedEdge,
  nodeNames,
  onUpdateNode,
  onUpdateEdge,
  bundleMeta,
  onUpdateMeta,
  onUpdateRunType,
  entry,
  onSetEntry,
  onDeleteNode,
  onDeleteEdge,
  issues,
  onFocusIssue,
}: {
  selectedNode: AgentNode | null
  selectedEdge: BundleEdge | null
  nodeNames: string[]
  onUpdateNode: (id: string, patch: Partial<AgentNode['data']>) => void
  onUpdateEdge: (id: string, patch: Partial<BundleEdge['data']>) => void
  onDeleteNode: (id: string) => void
  onDeleteEdge: (id: string) => void
  bundleMeta: { bundle: string; version: string; description: string; runType: BundleRunType }
  onUpdateMeta: (patch: Partial<{ bundle: string; version: string; description: string }>) => void
  onUpdateRunType: (runType: BundleRunType) => void
  entry: string | null
  onSetEntry: (id: string) => void
  issues: ValidationIssue[]
  onFocusIssue: (target: string) => void
}) {
  if (selectedNode && selectedNode.type === 'agentNode') {
    const gate = selectedNode.data.gate
    const isEntry = entry === selectedNode.id
    return (
      <div className="flex h-full flex-col gap-space-5 overflow-y-auto border-l border-border bg-surface p-space-5">
        <div>
          <p className="text-label-md text-ink-900">节点</p>
          <p className="text-ref text-ink-700">{selectedNode.data.alias || selectedNode.data.ref}</p>
          {selectedNode.data.alias && (
            <p className="text-caption text-ink-500">来自 {selectedNode.data.ref}</p>
          )}
        </div>

        {/* Both of these used to live somewhere else: entry only in the
            Bundle panel's dropdown (which meant deselecting the node you
            were looking at), delete only on the keyboard. */}
        <div className="flex flex-col gap-space-2">
          <Button
            variant={isEntry ? 'secondary' : 'outline'}
            size="sm"
            disabled={isEntry}
            onClick={() => onSetEntry(selectedNode.id)}
          >
            <Play className="mr-1 size-3.5" aria-hidden />
            {isEntry ? '这是入口节点' : '设为入口节点'}
          </Button>
          <Button variant="ghost" size="sm" className="text-ink-500 hover:text-rust" onClick={() => onDeleteNode(selectedNode.id)}>
            <Trash2 className="mr-1 size-3.5" aria-hidden />
            删除这个节点
          </Button>
        </div>

        <div className="flex items-center gap-space-2">
          <Checkbox
            id="gate-enabled"
            checked={!!gate}
            onCheckedChange={(v) =>
              onUpdateNode(selectedNode.id, {
                gate: v === true ? { on_timeout: 'abort' } : undefined,
              })
            }
          />
          <label htmlFor="gate-enabled" className="text-label-md text-ink-700">
            human gate（此节点完成后需要审批）
          </label>
        </div>

        {gate && (
          <>
            <div className="flex flex-col gap-space-2">
              <label htmlFor="gate-timeout" className="text-label-md text-ink-700">
                超时秒数
              </label>
              <Input
                id="gate-timeout"
                type="number"
                value={gate.timeout_seconds ?? ''}
                onChange={(e) =>
                  onUpdateNode(selectedNode.id, {
                    gate: { ...gate, timeout_seconds: e.target.value ? Number(e.target.value) : undefined },
                  })
                }
                className="h-9"
                placeholder="1800"
              />
            </div>
            <div className="flex flex-col gap-space-2">
              <label htmlFor="gate-on-timeout" className="text-label-md text-ink-700">
                超时策略
              </label>
              <Select value={gate.on_timeout} onValueChange={(v) => onUpdateNode(selectedNode.id, { gate: { ...gate, on_timeout: v as typeof gate.on_timeout } })}>
                <SelectTrigger id="gate-on-timeout" className="h-9 w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="abort">中止</SelectItem>
                  <SelectItem value="auto_approve">自动通过</SelectItem>
                  <SelectItem value="auto_reject">自动驳回</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </>
        )}
      </div>
    )
  }

  if (selectedEdge) {
    const condition = selectedEdge.data?.condition ?? ''
    const error = condition ? validateCondition(condition) : null
    return (
      <div className="flex h-full flex-col gap-space-5 overflow-y-auto border-l border-border bg-surface p-space-5">
        <div className="flex items-start justify-between gap-space-2">
          <div>
            <p className="text-label-md text-ink-900">连线</p>
            <p className="text-ref text-ink-700">
              {selectedEdge.source} → {selectedEdge.target}
            </p>
          </div>
          <Button
            variant="ghost"
            size="sm"
            aria-label="删除这条连线"
            className="text-ink-500 hover:text-rust"
            onClick={() => onDeleteEdge(selectedEdge.id)}
          >
            <Trash2 className="size-3.5" aria-hidden />
          </Button>
        </div>

        <div className="flex flex-col gap-space-2">
          <label htmlFor="edge-condition" className="text-label-md text-ink-700">
            condition（可选，expr-lang）
          </label>
          <Input
            id="edge-condition"
            value={condition}
            onChange={(e) => onUpdateEdge(selectedEdge.id, { condition: e.target.value || undefined })}
            aria-invalid={!!error}
            className={error ? 'h-9 border-rust' : 'h-9'}
            placeholder="shared_state.tests_passed == true"
          />
          {error && <p className="text-caption text-rust">{error}</p>}
        </div>

        <div className="flex flex-col gap-space-2">
          <label htmlFor="edge-join" className="text-label-md text-ink-700">
            join（多路汇合时）
          </label>
          <Select
            value={selectedEdge.data?.join ?? 'none'}
            onValueChange={(v) => onUpdateEdge(selectedEdge.id, { join: v === 'none' ? undefined : (v as 'wait_all' | 'wait_any') })}
          >
            <SelectTrigger id="edge-join" className="h-9 w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">不设置</SelectItem>
              <SelectItem value="wait_all">wait_all</SelectItem>
              <SelectItem value="wait_any">wait_any</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col gap-space-5 border-l border-border bg-surface p-space-5">
      <div>
        <p className="text-label-md mb-space-3 text-ink-900">Bundle 属性</p>
        <div className="flex flex-col gap-space-3">
          <div className="flex flex-col gap-space-2">
            <label htmlFor="meta-bundle" className="text-label-md text-ink-700">
              bundle
            </label>
            <Input id="meta-bundle" value={bundleMeta.bundle} onChange={(e) => onUpdateMeta({ bundle: e.target.value })} className="h-9" />
          </div>
          <div className="flex flex-col gap-space-2">
            <label htmlFor="meta-version" className="text-label-md text-ink-700">
              version
            </label>
            <Input id="meta-version" value={bundleMeta.version} onChange={(e) => onUpdateMeta({ version: e.target.value })} className="h-9" />
          </div>
          <div className="flex flex-col gap-space-2">
            <label htmlFor="meta-desc" className="text-label-md text-ink-700">
              描述
            </label>
            <Input id="meta-desc" value={bundleMeta.description} onChange={(e) => onUpdateMeta({ description: e.target.value })} className="h-9" />
          </div>
          <div className="flex flex-col gap-space-2">
            <label htmlFor="meta-run-type" className="text-label-md text-ink-700">
              运行类型
            </label>
            <Select value={bundleMeta.runType} onValueChange={(v) => onUpdateRunType(v as BundleRunType)}>
              <SelectTrigger id="meta-run-type" className="h-9 w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="graph">graph（图编排）</SelectItem>
                <SelectItem value="flow">flow（顺序流程）</SelectItem>
                <SelectItem value="single">single（单体）</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-body-sm text-ink-500">{RUN_TYPE_HELP[bundleMeta.runType]}</p>
          </div>
          {bundleMeta.runType === 'graph' && (
            <div className="flex flex-col gap-space-2">
              <label htmlFor="meta-entry" className="text-label-md text-ink-700">
                entry（入口节点）
              </label>
              <Select value={entry ?? undefined} onValueChange={onSetEntry}>
                <SelectTrigger id="meta-entry" className="h-9 w-full">
                  <SelectValue placeholder="选择入口节点" />
                </SelectTrigger>
                <SelectContent>
                  {nodeNames.map((n) => (
                    <SelectItem key={n} value={n}>
                      {n}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
          {bundleMeta.runType === 'single' && nodeNames.length > 1 && (
            <p className="text-body-sm text-rust">single 类型只会运行第一个 agent（{nodeNames[0]}），其余节点会被忽略。</p>
          )}
        </div>
      </div>

      {issues.length > 0 && (
        <div>
          <p className="text-label-md mb-space-2 text-rust">校验问题（{issues.length}）</p>
          <ul className="flex flex-col gap-space-2">
            {issues.map((issue, i) => (
              <li key={i}>
                <Button variant="outline" size="sm" className="h-auto w-full flex-col items-start gap-0.5 whitespace-normal border-rust px-space-3 py-space-2 text-left" onClick={() => onFocusIssue(issue.target)}>
                  <span className="text-ref text-rust">{issue.target}</span>
                  <span className="text-body-sm text-ink-700">{issue.reason}</span>
                </Button>
              </li>
            ))}
          </ul>
        </div>
      )}

      {issues.length === 0 && <p className="text-body-sm text-ink-500">选中一个节点或连线以配置它的属性。</p>}
    </div>
  )
}
