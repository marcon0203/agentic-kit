import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Panel, Ref, Section } from '@/components/common/Page'
import { EmptyRail } from '@/components/common/Rail'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import { Can, useHasPermission } from '@/lib/rbac/usePermissions'
import type { components } from '@/lib/api/schema'

type Role = components['schemas']['Role']
type Permission = components['schemas']['Permission']

const MODULE_LABEL: Record<string, string> = {
  iam: '用户与权限',
  model_catalog: '模型提供商',
}

/**
 * 系统配置 → 角色权限：一个角色就是一组权限（每个权限对应界面上的一个具体
 * 按钮/操作），新增角色时勾选它能做什么，之后把角色分配给用户（在用户管理
 * 页完成）。权限本身是代码里预置的固定目录，不能在这里新增或删除——能改
 * 的是"哪个角色拥有哪些权限"和"哪个用户拥有哪些角色"。
 */
export function RolesPage() {
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [editingRole, setEditingRole] = useState<Role | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const canManage = useHasPermission('iam.role.manage')

  const rolesQuery = useQuery({
    queryKey: ['roles'],
    queryFn: async () => unwrap<Role[]>(await apiClient.GET('/roles', {})),
  })
  const permissionsQuery = useQuery({
    queryKey: ['permissions'],
    queryFn: async () => unwrap<Permission[]>(await apiClient.GET('/permissions', {})),
  })

  async function deleteRole(r: Role) {
    setActionError(null)
    try {
      unwrap(await apiClient.DELETE('/roles/{id}', { params: { path: { id: r.id } } }))
      queryClient.invalidateQueries({ queryKey: ['roles'] })
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '删除没能完成，请再试一次')
    }
  }

  const roles = rolesQuery.data ?? []
  const permissions = permissionsQuery.data ?? []
  const permissionByKey = new Map(permissions.map((p) => [p.key, p]))

  return (
    <div className="flex flex-col gap-space-6">
      <Section
        title="角色列表"
        aside={
          <Can permission="iam.role.manage">
            <Button className="bg-gradient-cta text-white hover:opacity-90" onClick={() => setCreateOpen(true)}>
              新增角色
            </Button>
          </Can>
        }
      >
        {actionError && (
          <p role="alert" className="text-body-sm text-rust">
            {actionError}
          </p>
        )}

        {(rolesQuery.isLoading || permissionsQuery.isLoading) && <ListSkeleton />}
        {(rolesQuery.isError || permissionsQuery.isError) && (
          <ErrorPanel message="角色列表没能加载出来" onRetry={() => { rolesQuery.refetch(); permissionsQuery.refetch() }} />
        )}

        {rolesQuery.isSuccess && roles.length === 0 && (
          <EmptyRail
            title="还没有创建任何角色"
            description="一个角色就是一组权限，例如「运营」可以管理模型 Provider 但不能管理用户。创建后去用户管理页分配给具体的人。"
            action={
              canManage ? (
                <Button size="sm" className="bg-gradient-cta text-white hover:opacity-90" onClick={() => setCreateOpen(true)}>
                  新增角色
                </Button>
              ) : undefined
            }
          />
        )}

        {roles.length > 0 && (
          <ul className="flex flex-col gap-space-3">
            {roles.map((r) => (
              <li key={r.id}>
                <Panel>
                  <div className="flex items-start justify-between gap-space-4">
                    <div className="flex min-w-0 flex-col gap-1">
                      <span className="flex items-center gap-space-2">
                        <span className="text-body-md text-ink-900">{r.name}</span>
                        <Ref tone="muted">{r.key}</Ref>
                      </span>
                      {r.description && <span className="text-body-sm text-ink-700">{r.description}</span>}
                      <span className="mt-space-1 flex flex-wrap gap-1">
                        {r.permission_keys.length === 0 && (
                          <span className="text-caption text-ink-500">未授予任何权限</span>
                        )}
                        {r.permission_keys.map((key) => (
                          <span key={key} className="text-caption rounded-full bg-surface-muted px-space-2 py-0.5 text-ink-700">
                            {permissionByKey.get(key)?.name ?? key}
                          </span>
                        ))}
                      </span>
                    </div>
                    <Can permission="iam.role.manage">
                      <div className="flex shrink-0 items-center gap-space-2">
                        <Button variant="outline" size="sm" onClick={() => setEditingRole(r)}>
                          编辑权限
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => deleteRole(r)}>
                          删除
                        </Button>
                      </div>
                    </Can>
                  </div>
                </Panel>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <RoleDialog
        mode="create"
        permissions={permissions}
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSaved={() => {
          queryClient.invalidateQueries({ queryKey: ['roles'] })
          setCreateOpen(false)
        }}
      />
      {editingRole && (
        <RoleDialog
          mode="edit"
          role={editingRole}
          permissions={permissions}
          open={!!editingRole}
          onOpenChange={(v) => !v && setEditingRole(null)}
          onSaved={() => {
            queryClient.invalidateQueries({ queryKey: ['roles'] })
            setEditingRole(null)
          }}
        />
      )}
    </div>
  )
}

function RoleDialog({
  mode,
  role,
  permissions,
  open,
  onOpenChange,
  onSaved,
}: {
  mode: 'create' | 'edit'
  role?: Role
  permissions: Permission[]
  open: boolean
  onOpenChange: (v: boolean) => void
  onSaved: () => void
}) {
  const [key, setKey] = useState(role?.key ?? '')
  const [name, setName] = useState(role?.name ?? '')
  const [description, setDescription] = useState(role?.description ?? '')
  const [selected, setSelected] = useState<Set<string>>(new Set(role?.permission_keys ?? []))
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  const byModule = new Map<string, Permission[]>()
  for (const p of permissions) {
    const list = byModule.get(p.module) ?? []
    list.push(p)
    byModule.set(p.module, list)
  }

  function toggle(permKey: string) {
    const next = new Set(selected)
    if (next.has(permKey)) next.delete(permKey)
    else next.add(permKey)
    setSelected(next)
  }

  function reset() {
    setKey(role?.key ?? '')
    setName(role?.name ?? '')
    setDescription(role?.description ?? '')
    setSelected(new Set(role?.permission_keys ?? []))
    setError(null)
  }

  async function submit() {
    setPending(true)
    setError(null)
    try {
      if (mode === 'create') {
        unwrap(
          await apiClient.POST('/roles', {
            body: { key, name, description: description || undefined, permission_keys: Array.from(selected) },
          }),
        )
      } else if (role) {
        unwrap(
          await apiClient.PATCH('/roles/{id}/permissions', {
            params: { path: { id: role.id } },
            body: { permission_keys: Array.from(selected) },
          }),
        )
      }
      reset()
      onSaved()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '保存失败，请稍后重试')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) reset()
      }}
    >
      <DialogContent className="max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{mode === 'create' ? '新增角色' : `编辑权限 · ${role?.name}`}</DialogTitle>
          <DialogDescription>勾选这个角色能做的每一个具体操作。</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-space-3">
          {mode === 'create' && (
            <>
              <label htmlFor="role-key" className="text-label-md text-ink-700">
                Key（英文标识，如 operator）
              </label>
              <Input id="role-key" value={key} onChange={(e) => setKey(e.target.value)} className="h-12 rounded-sm" />

              <label htmlFor="role-name" className="text-label-md text-ink-700">
                名称
              </label>
              <Input id="role-name" value={name} onChange={(e) => setName(e.target.value)} className="h-12 rounded-sm" />

              <label htmlFor="role-description" className="text-label-md text-ink-700">
                描述（可选）
              </label>
              <Textarea id="role-description" value={description} onChange={(e) => setDescription(e.target.value)} rows={2} />
            </>
          )}

          <span className="text-label-md text-ink-700">权限</span>
          {Array.from(byModule.entries()).map(([module, perms]) => (
            <div key={module} className="flex flex-col gap-1">
              <span className="text-caption text-ink-500">{MODULE_LABEL[module] ?? module}</span>
              {perms.map((p) => (
                <label key={p.key} className="flex items-center gap-space-2 rounded-sm px-space-2 py-1 hover:bg-surface-muted">
                  <Checkbox checked={selected.has(p.key)} onCheckedChange={() => toggle(p.key)} />
                  <span className="text-body-sm text-ink-900">{p.name}</span>
                </label>
              ))}
            </div>
          ))}

          {error && (
            <p role="alert" className="text-caption text-rust">
              {error}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            取消
          </Button>
          <Button disabled={pending || (mode === 'create' && (!key || !name))} onClick={submit}>
            {pending ? '保存中…' : '保存'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
