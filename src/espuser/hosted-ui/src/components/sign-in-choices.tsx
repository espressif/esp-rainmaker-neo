/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { JSX } from 'preact'
import type { Brand } from '../page-data.ts'
import { BRAND_NAMES, type Option } from '../lib/select-screen.ts'
import { opensHere } from '../lib/navigation.ts'
import { AppleMark, ChevronMark, GenericMark, GitHubMark, GoogleMark, MailMark } from './marks.tsx'

const BRAND_MARKS: Record<Brand, () => JSX.Element> = { google: GoogleMark, apple: AppleMark, github: GitHubMark }

/** The mark a provider is drawn with: its brand's own, else its own logo, else the generic shield. */
export function ProviderMark({ option }: { option: Option }) {
  if (option.brand) {
    const Mark = BRAND_MARKS[option.brand]
    return <Mark />
  }
  // Only an inline image: the server admits nothing else, and the page's CSP would block anything remote.
  if (option.logo?.startsWith('data:image/')) return <img class="provider-logo" src={option.logo} alt="" />
  return <GenericMark />
}

/** A provider that signs in elsewhere: a brand's own button, or a plain row naming it; a plain click hands the page over to the redirect screen. */
export function RedirectButton({ option, onLeave }: { option: Option; onLeave: (option: Option) => void }) {
  const click = (e: MouseEvent) => { if (opensHere(e)) onLeave(option) }
  if (option.brand) {
    return (
      <a class={`social social--${option.brand}`} href={option.href} onClick={click}>
        <ProviderMark option={option} /><span>Sign in with {BRAND_NAMES[option.brand]}</span>
      </a>
    )
  }
  return (
    <a class="method" href={option.href} onClick={click}>
      <span class="mark"><ProviderMark option={option} /></span><span>Continue with {option.label}</span>
    </a>
  )
}

/** The one-time code, set apart from the brand pills: it is ours, so it says what will happen before the person commits. */
export function CodeButton({ onClick }: { onClick: () => void }) {
  return (
    <button type="button" class="code-option" onClick={onClick}>
      <span class="code-option-icon"><MailMark /></span>
      <span class="code-option-text">
        <span class="code-option-title">Sign in with a code</span>
        <span class="code-option-hint">We’ll email or text you a one-time code</span>
      </span>
      <ChevronMark />
    </button>
  )
}

/** A password pool, listed only when there are several; the pressed one is the account the form signs in to. */
export function PoolButton({ pool, selected, onSelect }: { pool: Option; selected: boolean; onSelect: (id: string) => void }) {
  return (
    <button type="button" class="method" aria-pressed={selected} onClick={() => onSelect(pool.id)}>
      <span class="mark"><ProviderMark option={pool} /></span><span>{pool.label}</span>
    </button>
  )
}
