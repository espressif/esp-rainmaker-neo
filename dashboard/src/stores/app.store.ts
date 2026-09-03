/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { appConfig, appStorageKey } from '@/lib/app-config'
import urlParamsConfig from '@/config/url-params.config.json'
import { getURLParamValue } from '@/utils/utils'
import type { SupportedLanguage } from '@/lib/app-config'

const { SIDEBAR_COLLAPSED, DARK_MODE, LANGUAGE } = urlParamsConfig

// Get initial values from URL params or fall back to config defaults
const getInitialSidebarCollapsed = (): boolean => {
  const urlValue = getURLParamValue(SIDEBAR_COLLAPSED)
  return urlValue !== undefined ? urlValue === 'true' : appConfig.defaults.sidebarCollapsed
}

const getInitialDarkMode = (): boolean => {
  const urlValue = getURLParamValue(DARK_MODE)
  return urlValue !== undefined ? urlValue === 'true' : appConfig.defaults.darkMode
}

const getInitialLanguage = (): SupportedLanguage => {
  const urlValue = getURLParamValue(LANGUAGE)
  return (urlValue as SupportedLanguage) || appConfig.defaults.language
}

interface AppState {
  sidebarCollapsed: boolean
  setSidebarCollapsed: (collapsed: boolean) => void
  toggleSidebar: () => void
  darkMode: boolean
  setDarkMode: (enabled: boolean) => void
  toggleDarkMode: () => void
  language: SupportedLanguage
  setLanguage: (lang: SupportedLanguage) => void
}

export const useAppStore = create<AppState>()(
  persist(
    (set) => ({
      sidebarCollapsed: getInitialSidebarCollapsed(),
      setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
      toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
      darkMode: getInitialDarkMode(),
      setDarkMode: (enabled) => set({ darkMode: enabled }),
      toggleDarkMode: () => set((state) => ({ darkMode: !state.darkMode })),
      language: getInitialLanguage(),
      setLanguage: (lang) => set({ language: lang }),
    }),
    {
      name: appStorageKey('app-storage'),
    }
  )
)

