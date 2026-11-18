/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

// Brand marks ship with the app, never from the operator-written registry.

/** Google's four-colour G, never recoloured: their guidelines forbid it. */
export const GoogleMark = () => (
  <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
    <path fill="#4285F4" d="M23.5 12.3c0-.8-.1-1.6-.2-2.3H12v4.5h6.5a5.6 5.6 0 0 1-2.4 3.7v3h3.9c2.3-2.1 3.5-5.2 3.5-8.9z"/>
    <path fill="#34A853" d="M12 24c3.2 0 5.9-1.1 7.9-2.9l-3.9-3a7.2 7.2 0 0 1-10.7-3.8h-4v3.1A12 12 0 0 0 12 24z"/>
    <path fill="#FBBC05" d="M5.3 14.3a7.1 7.1 0 0 1 0-4.6V6.6h-4a12 12 0 0 0 0 10.8l4-3.1z"/>
    <path fill="#EA4335" d="M12 4.8c1.8 0 3.4.6 4.6 1.8l3.5-3.5A12 12 0 0 0 1.3 6.6l4 3.1A7.2 7.2 0 0 1 12 4.8z"/>
  </svg>
)

export const GitHubMark = () => (
  <svg viewBox="0 0 16 16" width="18" height="18" fill="currentColor" aria-hidden="true">
    <path d="M8 0a8 8 0 0 0-2.5 15.6c.4.1.5-.2.5-.4v-1.4C3.8 14.2 3.3 13 3.3 13c-.4-1-.9-1.2-.9-1.2-.8-.5 0-.5 0-.5.8.1 1.3.9 1.3.9.7 1.3 1.9.9 2.4.7.1-.5.3-.9.5-1.1-1.8-.2-3.6-.9-3.6-4 0-.9.3-1.6.8-2.1 0-.2-.3-1 .1-2.1 0 0 .7-.2 2.2.8a7.5 7.5 0 0 1 4 0c1.5-1 2.2-.8 2.2-.8.4 1.1.1 1.9.1 2.1.5.5.8 1.2.8 2.1 0 3.1-1.8 3.8-3.6 4 .3.3.5.8.5 1.5v2.2c0 .2.1.5.6.4A8 8 0 0 0 8 0z"/>
  </svg>
)

/** Drawn for any provider without a mark of its own. */
export const GenericMark = () => (
  <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.6"
       strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M12 2L4 6v6c0 5 3.4 8.4 8 10 4.6-1.6 8-5 8-10V6z"/><path d="M9 12l2 2 4-4"/>
  </svg>
)

export const ChevronMark = () => (
  <svg class="chevron" viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.8"
       strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="m9 6 6 6-6 6"/>
  </svg>
)

export const MailMark = () => (
  <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.6"
       strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <rect x="3" y="5" width="18" height="14" rx="2"/><path d="m3 7 9 6 9-6"/>
  </svg>
)

/** Apple's mark; it follows the button's text colour, which flips with the theme as Apple's guidelines ask. */
export const AppleMark = () => (
  <svg viewBox="0 0 24 24" width="17" height="17" fill="currentColor" aria-hidden="true">
    <path d="M16.37 1.43c0 1.14-.42 2.2-1.25 3.04-.9.92-1.98 1.45-3.16 1.36-.15-1.1.43-2.27 1.2-3.06.86-.88 2.3-1.53 3.21-1.34zM20.5 17.2c-.55 1.27-.81 1.84-1.52 2.96-.99 1.57-2.38 3.52-4.1 3.53-1.53.02-1.93-1-4-.99-2.08.01-2.51 1.01-4.04.99-1.73-.02-3.04-1.78-4.03-3.35C.04 16.06-.25 10.93 1.52 8.21 2.78 6.28 4.77 5.15 6.64 5.15c1.9 0 3.1 1.04 4.67 1.04 1.53 0 2.46-1.04 4.66-1.04 1.66 0 3.43.91 4.69 2.48-4.12 2.26-3.45 8.14.84 9.57z"/>
  </svg>
)

/** Drawn, like WarnMark, so an assist line's mark renders the same on every platform. */
export const AlertMark = () => (
  <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="2"
       strokeLinecap="round" aria-hidden="true">
    <circle cx="12" cy="12" r="9"/><path d="M12 7.5v5.5M12 16.5v.01"/>
  </svg>
)

export const WarnMark = () => (
  <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="2"
       strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M12 3.5 2.5 20h19z"/><path d="M12 10v4.5M12 17.5v.01"/>
  </svg>
)

/** Show/hide password, drawn rather than an emoji so it renders the same everywhere. */
export const EyeMark = ({ off }: { off?: boolean }) => (
  <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.6"
       strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M2 12s3.6-6.5 10-6.5S22 12 22 12s-3.6 6.5-10 6.5S2 12 2 12Z"/>
    <circle cx="12" cy="12" r="2.6"/>
    {off ? <path d="m4 4 16 16"/> : null}
  </svg>
)

/** The Espressif symbol: the wordmark's lead and, alone, the account's side of the consent screen. */
const SYMBOL = (
  <>
    <path d="M6.93,15.65a1.48,1.48,0,1,1-1.48-1.48,1.48,1.48,0,0,1,1.48,1.48" fill="#e7352c"/>
    <path d="M19.84,14.11a15.34,15.34,0,0,0-13-13A9.73,9.73,0,0,0,4.69,2.66V4.09A12.18,12.18,0,0,1,16.86,16.25h1.43a10.25,10.25,0,0,0,1.55-2.14" fill="#e7352c"/>
    <path d="M21,9.62A9.62,9.62,0,0,0,11.33,0c-.34,0-.67,0-1,.05l-.22.64A16.55,16.55,0,0,1,20.25,10.82l.64-.23c0-.32.06-.64.06-1" fill="#e7352c"/>
    <path d="M11.44,21A11.44,11.44,0,0,1,0,9.61,11.36,11.36,0,0,1,3.35,1.52L4,2.13a10.56,10.56,0,0,0,0,15,10.56,10.56,0,0,0,15,0l.61.61A11.35,11.35,0,0,1,11.44,21" fill="#e7352c"/>
    <path d="M11.31,16.91A6.65,6.65,0,0,0,8.7,11,6.51,6.51,0,0,0,5.35,9.64.61.61,0,0,1,4.82,9a.59.59,0,0,1,.65-.54,7.85,7.85,0,0,1,7,8.58,6.85,6.85,0,0,1-.26,1.35l1.74.49a9.66,9.66,0,0,0,1.5-.57,10.64,10.64,0,0,0,.19-2.05A11,11,0,0,0,6.31,5.4a4.84,4.84,0,0,0-1.75,0A3.81,3.81,0,0,0,2.67,6.57a3.74,3.74,0,0,0,1.67,6,8.66,8.66,0,0,0,.94.17h0a3.51,3.51,0,0,1,2.92,3.46,3.47,3.47,0,0,1-.56,1.89l1.2.77a8.91,8.91,0,0,0,1.8.3,6.58,6.58,0,0,0,.66-2.3" fill="#e7352c"/>
  </>
)

export const EspressifMark = () => (
  <svg viewBox="0 0 21.04 21.04" width="28" height="28" role="img" aria-label="Espressif">{SYMBOL}</svg>
)

/** Stands in for an app that registered no logo, so the screen never shows a blank tile. */
export const AppMark = () => (
  <svg viewBox="0 0 24 24" width="28" height="28" fill="none" stroke="currentColor" strokeWidth="1.6"
       strokeLinejoin="round" aria-hidden="true">
    <rect x="3.5" y="3.5" width="7" height="7" rx="2"/><rect x="13.5" y="3.5" width="7" height="7" rx="2"/>
    <rect x="3.5" y="13.5" width="7" height="7" rx="2"/><rect x="13.5" y="13.5" width="7" height="7" rx="3.5"/>
  </svg>
)

/** The Espressif wordmark, inline so it needs no request and its lettering follows currentColor. */
export const Wordmark = () => (
  <svg viewBox="0 0 116.84 21.04" role="img" aria-label="Espressif">
    {SYMBOL}
    <rect x="104.18" y="3.17" width="2.21" height="14.7" fill="currentColor"/>
    <path d="M68.39,11.73a2.51,2.51,0,0,0,1.46-2.41V5.81a2.64,2.64,0,0,0-.78-1.95,2.6,2.6,0,0,0-1.78-.7H60.91V17.89h2.22V12H65.4a2,2,0,0,1,2,1.74l.87,4.17h2.42L69.75,14a3,3,0,0,0-1.36-2.22M63.13,5.24v0h3.8a.75.75,0,0,1,.71.84v3a.75.75,0,0,1-.71.84h-3.8Z" fill="currentColor"/>
    <path d="M29.09,3.87a2.67,2.67,0,0,0-.78,2v9.4a2.64,2.64,0,0,0,.78,1.95,2.62,2.62,0,0,0,1.78.7h6V15.82h-5.6a.76.76,0,0,1-.72-.84V11.2H36V9.14H30.52V6.08a.76.76,0,0,1,.72-.84h5.6V3.17h-6a2.66,2.66,0,0,0-1.78.7" fill="currentColor"/>
    <path d="M111.24,5.24h5.6V3.17h-6a2.66,2.66,0,0,0-1.78.7,2.71,2.71,0,0,0-.78,2v12h2.21V11.2H116V9.14h-5.52V6.08a.76.76,0,0,1,.72-.84" fill="currentColor"/>
    <path d="M45.2,9.14H41.77A.79.79,0,0,1,41,8.35V6.08a.75.75,0,0,1,.71-.84H47.2V3.17H41.33a2.66,2.66,0,0,0-1.78.7,2.67,2.67,0,0,0-.78,2V8.88a2.59,2.59,0,0,0,2.58,2.59h3.44a.79.79,0,0,1,.79.79V15a.76.76,0,0,1-.72.84H38.77v2.07h6.46a2.55,2.55,0,0,0,2.56-2.66v-3.5A2.58,2.58,0,0,0,45.2,9.14" fill="currentColor"/>
    <path d="M72.65,3.87a2.67,2.67,0,0,0-.78,2v9.4a2.64,2.64,0,0,0,.78,1.95,2.62,2.62,0,0,0,1.78.7h6V15.82h-5.6a.75.75,0,0,1-.71-.84V11.2h5.51V9.14H74.08V6.08a.75.75,0,0,1,.71-.84h5.6V3.17h-6a2.66,2.66,0,0,0-1.78.7" fill="currentColor"/>
    <path d="M88.76,9.14H85.33a.79.79,0,0,1-.79-.79V6.08a.75.75,0,0,1,.71-.84h5.51V3.17H84.88a2.64,2.64,0,0,0-1.77.7,2.67,2.67,0,0,0-.78,2V8.88a2.58,2.58,0,0,0,2.58,2.59h3.43a.79.79,0,0,1,.79.79V15a.75.75,0,0,1-.71.84H82.33v2.07h6.46a2.6,2.6,0,0,0,1.77-.71,2.63,2.63,0,0,0,.79-2v-3.5a2.58,2.58,0,0,0-2.59-2.58" fill="currentColor"/>
    <path d="M99.6,9.14H96.17a.79.79,0,0,1-.79-.79V6.08a.75.75,0,0,1,.71-.84h5.51V3.17H95.73a2.66,2.66,0,0,0-1.78.7,2.67,2.67,0,0,0-.78,2V8.88a2.59,2.59,0,0,0,2.58,2.59h3.44a.79.79,0,0,1,.79.79V15a.76.76,0,0,1-.72.84H93.17v2.07h6.46a2.55,2.55,0,0,0,2.56-2.66v-3.5A2.58,2.58,0,0,0,99.6,9.14" fill="currentColor"/>
    <path d="M56.48,3.16H49.79V17.89H52V12.17h4.48a2.57,2.57,0,0,0,1.77-.7,2.63,2.63,0,0,0,.79-2V5.81a2.65,2.65,0,0,0-.79-1.95,2.57,2.57,0,0,0-1.77-.7m.34,6.11a.75.75,0,0,1-.71.84H52V5.22h4.11a.75.75,0,0,1,.71.84Z" fill="currentColor"/>
  </svg>
)
