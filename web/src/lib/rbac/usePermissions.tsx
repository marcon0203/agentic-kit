import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'

import { apiClient, unwrap } from '@/lib/api/client'
import { useAuthStore } from '@/lib/auth/store'

/**
 * GET /me/permissions is the frontend's one source of truth for button-
 * level RBAC: is_admin bypasses every check (mirrors the backend's
 * requireAccess), otherwise a button only renders/enables when its
 * permission key is in the set the current user's Roles grant. Fetched
 * once per session and cached — a 30s staleTime matches the query client's
 * default so admin-page actions (which invalidate ['catalog-providers']
 * etc., not this) don't force a refetch on every click.
 */
export function usePermissions() {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated())
  const query = useQuery({
    queryKey: ['me-permissions'],
    queryFn: async () =>
      unwrap<{ is_admin: boolean; permissions: string[] }>(await apiClient.GET('/me/permissions', {})),
    enabled: isAuthenticated,
  })
  const isAdmin = query.data?.is_admin ?? false
  const granted = new Set(query.data?.permissions ?? [])
  return {
    isAdmin,
    isLoading: query.isLoading,
    has: (key: string) => isAdmin || granted.has(key),
  }
}

export function useHasPermission(key: string): boolean {
  return usePermissions().has(key)
}

/**
 * Declarative button-level gate: renders `children` only when the current
 * user holds `permission` (or is_admin). Nothing renders while the
 * permission set is still loading — a button that flashes visible then
 * disappears is worse than one that appears a beat late.
 */
export function Can({ permission, children }: { permission: string; children: ReactNode }) {
  const { has, isLoading } = usePermissions()
  if (isLoading || !has(permission)) return null
  return <>{children}</>
}
