import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Ref, Section } from '@/components/common/Page'
import { EmptyRail } from '@/components/common/Rail'
import { ErrorPanel, ListSkeleton } from '@/components/common/EmptyState'
import { apiClient, unwrap, ApiError } from '@/lib/api/client'
import { cn } from '@/lib/utils'
import { Can, useHasPermission } from '@/lib/rbac/usePermissions'
import type { components } from '@/lib/api/schema'

type UserAccount = components['schemas']['UserAccount']
type Role = components['schemas']['Role']

/**
 * 系统配置 → 用户管理：启用/停用账号、分配角色。RBAC 真正的权限判定在
 * 后端（每个受控接口自己检查），这里的 <Can> 只负责按钮级别的显隐——
 * 一个没有对应权限的人不会在界面上看到那个按钮，而不是点了才被拒绝。
 */
export function UsersPage() {
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [actionError, setActionError] = useState<string | null>(null)
  const [assigningUser, setAssigningUser] = useState<UserAccount | null>(null)
  const canManageStatus = useHasPermission('iam.user.manage_status')

  const usersQuery = useQuery({
    queryKey: ['users'],
    queryFn: async () => unwrap<UserAccount[]>(await apiClient.GET('/users', {})),
  })

  async function toggleStatus(u: UserAccount) {
    setActionError(null)
    try {
      unwrap(
        await apiClient.PATCH('/users/{id}/status', {
          params: { path: { id: u.id } },
          body: { status: u.status === 1 ? 2 : 1 },
        }),
      )
      queryClient.invalidateQueries({ queryKey: ['users'] })
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : '操作没能完成，请再试一次')
    }
  }

  const users = usersQuery.data ?? []
  const filtered = search
    ? users.filter((u) => u.email.includes(search) || u.display_name.includes(search))
    : users

  return (
    <div className="flex flex-col gap-space-6">
      <Section title="用户列表">
        {users.length > 0 && (
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="按邮箱或显示名筛选"
            className="max-w-xs"
          />
        )}

        {actionError && (
          <p role="alert" className="text-body-sm text-rust">
            {actionError}
          </p>
        )}

        {usersQuery.isLoading && <ListSkeleton />}
        {usersQuery.isError && <ErrorPanel message="用户列表没能加载出来" onRetry={() => usersQuery.refetch()} />}

        {usersQuery.isSuccess && filtered.length === 0 && (
          <EmptyRail title="没有匹配的用户" description="筛选只匹配邮箱和显示名。" />
        )}

        {filtered.length > 0 && (
          <ul className="overflow-hidden rounded-lg border border-border bg-surface">
            {filtered.map((u) => (
              <li key={u.id} className="flex items-center gap-space-4 border-b border-border px-space-5 py-space-3 last:border-0">
                <span
                  aria-hidden
                  className={cn('size-2 shrink-0 rounded-full', u.status === 1 ? 'bg-moss' : 'bg-border-strong')}
                />
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="flex items-center gap-space-2">
                    <span className="text-body-md text-ink-900">{u.display_name}</span>
                    <span className="text-caption text-ink-500">{u.email}</span>
                    {u.is_admin && <span className="text-caption rounded-full bg-signal-tint px-space-2 py-0.5 text-signal">超级管理员</span>}
                  </span>
                  {u.role_keys.length > 0 && (
                    <span className="flex flex-wrap items-center gap-1">
                      {u.role_keys.map((k) => (
                        <Ref key={k} tone="muted">
                          {k}
                        </Ref>
                      ))}
                    </span>
                  )}
                </span>
                <span className={cn('text-caption w-12 shrink-0 text-right', u.status === 1 ? 'text-moss' : 'text-ink-500')}>
                  {u.status === 1 ? '已启用' : '已停用'}
                </span>
                <Can permission="iam.user.manage_roles">
                  <Button variant="outline" size="sm" onClick={() => setAssigningUser(u)}>
                    分配角色
                  </Button>
                </Can>
                {canManageStatus && !u.is_admin && (
                  <Button variant="outline" size="sm" onClick={() => toggleStatus(u)}>
                    {u.status === 1 ? '停用' : '启用'}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </Section>

      {assigningUser && (
        <AssignRolesDialog
          user={assigningUser}
          open={!!assigningUser}
          onOpenChange={(v) => !v && setAssigningUser(null)}
          onSaved={() => {
            queryClient.invalidateQueries({ queryKey: ['users'] })
            setAssigningUser(null)
          }}
        />
      )}
    </div>
  )
}

function AssignRolesDialog({
  user,
  open,
  onOpenChange,
  onSaved,
}: {
  user: UserAccount
  open: boolean
  onOpenChange: (v: boolean) => void
  onSaved: () => void
}) {
  const rolesQuery = useQuery({
    queryKey: ['roles'],
    queryFn: async () => unwrap<Role[]>(await apiClient.GET('/roles', {})),
  })
  const [selected, setSelected] = useState<Set<string> | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  const roles = rolesQuery.data ?? []
  // Lazily seed `selected` from the user's current roles once the role
  // catalog has loaded, so the checkbox list opens pre-checked.
  const current = selected ?? new Set(roles.filter((r) => user.role_keys.includes(r.key)).map((r) => r.id))

  function toggle(id: string) {
    const next = new Set(current)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setSelected(next)
  }

  async function submit() {
    setPending(true)
    setError(null)
    try {
      unwrap(
        await apiClient.PATCH('/users/{id}/roles', {
          params: { path: { id: user.id } },
          body: { role_ids: Array.from(current) },
        }),
      )
      onSaved()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '保存失败，请稍后重试')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>分配角色 · {user.display_name}</DialogTitle>
          <DialogDescription>勾选的角色会替换这个用户当前的角色分配。</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-space-2">
          {rolesQuery.isLoading && <ListSkeleton rows={2} />}
          {rolesQuery.isSuccess && roles.length === 0 && (
            <p className="text-body-sm text-ink-500">还没有创建任何角色，先去角色权限页面新增一个。</p>
          )}
          {roles.map((r) => (
            <label key={r.id} className="flex items-start gap-space-2 rounded-sm px-space-2 py-space-1 hover:bg-surface-muted">
              <Checkbox checked={current.has(r.id)} onCheckedChange={() => toggle(r.id)} className="mt-0.5" />
              <span className="flex flex-col">
                <span className="text-body-sm text-ink-900">{r.name}</span>
                {r.description && <span className="text-caption text-ink-500">{r.description}</span>}
              </span>
            </label>
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
          <Button disabled={pending} onClick={submit}>
            {pending ? '保存中…' : '保存'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
