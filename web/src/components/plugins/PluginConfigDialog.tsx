import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'

type PluginInstallation = components['schemas']['PluginInstallation']

/** 安装时要用户填的一项，对应清单里的 requires.config_schema[]。 */
export type PluginConfigField = {
  key: string
  label: string
  description?: string
  required?: boolean
  secret?: boolean
  placeholder?: string
  default?: string
}

/**
 * 已安装插件的配置：看当前值、改配置、换密钥。
 *
 * 非凭据项（endpoint、bucket 这些）当前值直接回填，改完存回去即可。凭据项
 * 的值从来不回显——响应里根本没有——所以按名字给一个空的密码框，**留空表
 * 示不改**。
 *
 * 这里有一处必须小心：后端把空串当作"清除这个密钥"，所以留空的密码框绝不
 * 能被提交，否则点一次保存就把 AccessKey 抹了。实现上只把用户真的动过的键
 * 放进请求体。
 */
export function PluginConfigDialog({
  open,
  onOpenChange,
  pluginID,
  displayName,
  installation,
  fields,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  pluginID: string
  displayName: string
  installation: PluginInstallation | undefined
  fields: PluginConfigField[]
}) {
  const queryClient = useQueryClient()
  const [values, setValues] = useState<Record<string, string>>({})
  const [secretDrafts, setSecretDrafts] = useState<Record<string, string>>({})

  const currentConfig = (installation?.config ?? {}) as Record<string, unknown>
  const credentialKeys = installation?.credential_keys ?? []

  useEffect(() => {
    if (!open) return
    // 非凭据项回填当前值；凭据项一律留空。
    const next: Record<string, string> = {}
    for (const f of fields) {
      if (f.secret) continue
      const v = (installation?.config ?? {})[f.key] as unknown
      next[f.key] = typeof v === 'string' ? v : v == null ? '' : String(v)
    }
    setValues(next)
    setSecretDrafts({})
  }, [open, installation, fields])

  const saveMutation = useMutation({
    mutationFn: async () => {
      // 先把 config 里表单没接管的键原样带回（比如连接器的
      // connector_resource_id），否则保存一次就把它们删了。
      const config: Record<string, unknown> = { ...currentConfig }
      for (const f of fields) {
        if (f.secret) continue
        const v = (values[f.key] ?? '').trim()
        if (v === '') delete config[f.key]
        else config[f.key] = v
      }
      // 只提交动过的凭据键。没动的根本不出现在请求体里，后端据此保留原值。
      for (const [k, v] of Object.entries(secretDrafts)) {
        config[k] = v
      }
      return unwrap<PluginInstallation>(
        await apiClient.PATCH('/plugins/{id}/install', {
          params: { path: { id: pluginID } },
          body: { config: config as Record<string, never> },
        }),
      )
    },
    onSuccess: () => {
      toast.success('配置已更新')
      queryClient.invalidateQueries({ queryKey: ['plugins'] })
      onOpenChange(false)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : '保存没能完成，请再试一次'),
  })

  const missingRequired = fields.some(
    (f) => f.required && !f.secret && (values[f.key] ?? '').trim() === '',
  )

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{displayName} 的配置</DialogTitle>
          <DialogDescription>改完点保存即可生效，下次调用就用新配置。密钥留空表示不改。</DialogDescription>
        </DialogHeader>

        <div className="flex max-h-[60vh] flex-col gap-space-4 overflow-y-auto">
          {fields.length === 0 && <p className="text-body-sm text-ink-500">这个插件没有声明可配置项。</p>}

          {fields.map((f) => {
            if (!f.secret) {
              return (
                <label key={f.key} className="flex flex-col gap-space-1">
                  <span className="text-caption text-ink-500">
                    {f.label}
                    {f.required && <span className="ml-1 text-rust">*</span>}
                  </span>
                  <Input
                    value={values[f.key] ?? ''}
                    onChange={(e) => setValues((prev) => ({ ...prev, [f.key]: e.target.value }))}
                    placeholder={f.placeholder}
                    className="h-9"
                  />
                  {f.description && <span className="text-caption text-ink-500">{f.description}</span>}
                </label>
              )
            }
            const configured = credentialKeys.includes(f.key)
            const touched = f.key in secretDrafts
            return (
              <label key={f.key} className="flex flex-col gap-space-1">
                <span className="text-caption text-ink-500">
                  {f.label}
                  {f.required && <span className="ml-1 text-rust">*</span>}
                  <span className="ml-space-2">
                    {configured ? <span className="text-moss">已配置</span> : <span className="text-rust">未配置</span>}
                  </span>
                </span>
                <Input
                  type="password"
                  autoComplete="new-password"
                  value={secretDrafts[f.key] ?? ''}
                  onChange={(e) => setSecretDrafts((prev) => ({ ...prev, [f.key]: e.target.value }))}
                  placeholder={configured ? '••••••••（不改就别填）' : '尚未配置，填一个'}
                  className="h-9"
                />
                {touched && secretDrafts[f.key] === '' && (
                  <span className="text-caption text-rust">保存后将清除这个密钥</span>
                )}
                {f.description && <span className="text-caption text-ink-500">{f.description}</span>}
              </label>
            )
          })}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button
            className="bg-gradient-cta text-white hover:opacity-90"
            disabled={saveMutation.isPending || missingRequired}
            onClick={() => saveMutation.mutate()}
          >
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
