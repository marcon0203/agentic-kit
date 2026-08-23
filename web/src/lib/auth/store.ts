import { create } from 'zustand'
import { persist } from 'zustand/middleware'

import type { components } from '@/lib/api/schema'

type AuthResult = components['schemas']['AuthResult']
type User = components['schemas']['User']

interface AuthState {
  accessToken: string | null
  refreshToken: string | null
  user: User | null
  /** Whether the auth modal is currently open, and why it was opened —
   * shown as copy inside the modal (e.g. "登录后继续" vs a plain "登录"). */
  modalOpen: boolean
  modalReason: 'manual' | 'protected-route' | 'session-expired' | null

  isAuthenticated: () => boolean
  setSession: (result: AuthResult) => void
  clearSession: () => void
  openModal: (reason?: AuthState['modalReason']) => void
  closeModal: () => void
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      accessToken: null,
      refreshToken: null,
      user: null,
      modalOpen: false,
      modalReason: null,

      isAuthenticated: () => get().accessToken !== null,

      setSession: (result) =>
        set({
          accessToken: result.access_token,
          refreshToken: result.refresh_token,
          user: result.user,
          modalOpen: false,
          modalReason: null,
        }),

      clearSession: () => set({ accessToken: null, refreshToken: null, user: null }),

      openModal: (reason = 'manual') => set({ modalOpen: true, modalReason: reason }),
      closeModal: () => set({ modalOpen: false, modalReason: null }),
    }),
    {
      name: 'agentic-kit-auth',
      partialize: (state) => ({
        accessToken: state.accessToken,
        refreshToken: state.refreshToken,
        user: state.user,
      }),
    },
  ),
)
