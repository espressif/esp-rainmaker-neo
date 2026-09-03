/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { appStorageKey } from '@/lib/app-config'

interface UserState {
  loggedInUserName: string | null
  setLoggedInUserName: (name: string) => void
  clearUser: () => void
}

export const useUserStore = create<UserState>()(
  persist(
    (set) => ({
      loggedInUserName: null,
      setLoggedInUserName: (name) => set({ loggedInUserName: name }),
      clearUser: () => set({ loggedInUserName: null }),
    }),
    {
      name: appStorageKey('user-storage'),
    }
  )
)
