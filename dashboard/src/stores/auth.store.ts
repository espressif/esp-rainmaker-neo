/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { create } from 'zustand'
import { persist, createJSONStorage } from 'zustand/middleware'
import { appStorageKey } from '@/lib/app-config'

export interface AwsCredentials {
  accessKeyId: string
  secretAccessKey: string
  sessionToken: string
  expiration: number
}

interface AuthState {
  credentials: AwsCredentials | null
  setCredentials: (creds: AwsCredentials) => void
  clearCredentials: () => void
  isCredentialsValid: () => boolean
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      credentials: null,
      setCredentials: (creds) => set({ credentials: creds }),
      clearCredentials: () => set({ credentials: null }),
      isCredentialsValid: () => {
        const { credentials } = get()
        if (!credentials) {return false}
        // expiration is in seconds (unix timestamp)
        return Date.now() < credentials.expiration * 1000
      },
    }),
    {
      name: appStorageKey('auth-credentials'),
      storage: createJSONStorage(() => sessionStorage),
    }
  )
)
