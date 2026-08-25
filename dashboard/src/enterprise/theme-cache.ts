/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Pre-paint re-apply of cached enterprise theming.
 *
 * An entitled plugin that themes the app (e.g. an accent-color feature) caches its
 * document-level CSS and `data-*` attributes under one localStorage key so the *next* load can
 * re-apply them synchronously from `main.tsx`, before first paint — the plugin itself only
 * loads well after login, which would otherwise flash the default theme on every refresh.
 *
 * The host side is generic and knows nothing about what the theming is: it injects whatever
 * CSS the cache holds (size-capped) and sets only `data-`-prefixed attributes on the root
 * element. The cache carries the same trust as any other same-origin localStorage value and is
 * cosmetic only. The loader clears it whenever a session resolves to community mode, so
 * revoked deployments self-heal on their next load.
 */

const THEME_CACHE_STORAGE_KEY = 'rmng-enterprise-theme-cache'
const THEME_CACHE_STYLE_ID = 'rmng-enterprise-theme-cache'
const MAX_CSS_LENGTH = 50_000
const ATTR_NAME_PATTERN = /^data-[a-z][a-z0-9-]*$/

interface CachedEnterpriseTheme {
  css: string
  attrs: Record<string, string>
}

/** Returns the cache entry, or null when it is missing or malformed in any way. */
function readCachedTheme(): CachedEnterpriseTheme | null {
  const raw = localStorage.getItem(THEME_CACHE_STORAGE_KEY)
  if (!raw) {
    return null
  }
  const parsed: unknown = JSON.parse(raw)
  if (typeof parsed !== 'object' || parsed === null) {
    return null
  }
  const { css, attrs } = parsed as { css?: unknown; attrs?: unknown }
  if (typeof css !== 'string' || css.length > MAX_CSS_LENGTH) {
    return null
  }
  if (typeof attrs !== 'object' || attrs === null) {
    return null
  }
  const entries = Object.entries(attrs)
  const allValid = entries.every(
    ([name, value]) => ATTR_NAME_PATTERN.test(name) && typeof value === 'string',
  )
  if (!allValid) {
    return null
  }
  return { css, attrs: attrs as Record<string, string> }
}

/** Called synchronously from `main.tsx` before React renders — must never break boot. */
export function applyCachedEnterpriseTheme(): void {
  try {
    const theme = readCachedTheme()
    if (!theme) {
      localStorage.removeItem(THEME_CACHE_STORAGE_KEY)
      return
    }
    const style = document.createElement('style')
    style.id = THEME_CACHE_STYLE_ID
    style.textContent = theme.css
    document.head.appendChild(style)
    for (const [name, value] of Object.entries(theme.attrs)) {
      document.documentElement.setAttribute(name, value)
    }
  } catch (error) {
    console.error('[enterprise] cached theme apply failed:', error)
    localStorage.removeItem(THEME_CACHE_STORAGE_KEY)
  }
}

export function clearCachedEnterpriseTheme(): void {
  try {
    const theme = readCachedTheme()
    for (const name of Object.keys(theme?.attrs ?? {})) {
      document.documentElement.removeAttribute(name)
    }
  } catch (error) {
    console.error('[enterprise] cached theme clear failed:', error)
  } finally {
    localStorage.removeItem(THEME_CACHE_STORAGE_KEY)
    document.getElementById(THEME_CACHE_STYLE_ID)?.remove()
  }
}
