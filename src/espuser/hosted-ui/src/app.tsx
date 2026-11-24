/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentChildren } from 'preact'
import { useRef, useState } from 'preact/hooks'
import { readPageData } from './page-data.ts'
import { signInOptions, initialPool, screenFor, clientLabel } from './lib/select-screen.ts'
import type { ShownError } from './api/errors.ts'
import { CodeButton, PoolButton, RedirectButton } from './components/sign-in-choices.tsx'
import { Assist } from './components/inputs.tsx'
import { AppMark, Wordmark } from './components/marks.tsx'
import { PasswordScreen } from './screens/password.tsx'
import { OTPScreen } from './screens/otp.tsx'
import { TerminalScreen } from './screens/terminal.tsx'
import { AccountScreen, ACCOUNT_STEPS, type AccountStep } from './screens/account.tsx'
import { ConsentScreen } from './screens/consent.tsx'
import { LeavingScreen, useLeaving } from './screens/leaving.tsx'
import { LogoutScreen, SignOutResult } from './screens/logout.tsx'
import { PageFooter } from './components/page-footer.tsx'

const page = readPageData()
const screen = screenFor(page)
const options = signInOptions(page)
const STALLED: ShownError = { message: 'Still waiting? Try again.', action: 'retry', severity: 'warning' }

export function App() {
  const [pool, setPool] = useState(() => initialPool(options.pools, page.idp))
  const [codeSignIn, setCodeSignIn] = useState(options.otp && page.idp === 'otp')
  // The sign-in this page was drawn for is over: it took too long, or a newer one in this browser replaced it.
  const [ended, setEnded] = useState<ShownError | null>(null)
  const [account, setAccount] = useState<AccountStep | null>(null)
  // One value for every screen, so an address typed once carries over when the person switches method.
  const [username, setUsername] = useState(page.login_hint ?? '')
  // Set only when an unverified account's sign-in resent its code, so the verify step can resend it again.
  const [provenPassword, setProvenPassword] = useState('')
  const leaving = useLeaving()
  const body = useRef<HTMLDivElement>(null)

  if (screen.kind === 'error') {
    return <Shell narrow title={null}><TerminalScreen code={screen.error.code} message={screen.error.message} restartURL={page.restart_url} /></Shell>
  }
  if (ended) {
    return (
      <Shell narrow title={null}>
        <TerminalScreen message={ended.message} restartURL={ended.action === 'restart' ? page.restart_url : undefined} />
      </Shell>
    )
  }
  if (screen.kind === 'logout') {
    return <Shell narrow title={null}><LogoutScreen username={screen.logout.username} csrfToken={screen.logout.csrf_token} /></Shell>
  }
  if (screen.kind === 'signed-out') {
    return <Shell narrow title={null}><SignOutResult outcome="signed-out" /></Shell>
  }
  if (screen.kind === 'consent') {
    return (
      <Shell narrow title={null}>
        <ConsentScreen
          appName={page.client.name || 'This app'} logoURI={page.client.logo_uri} username={screen.consent.username}
          scopes={screen.consent.scopes} flowID={page.flow_id} onEnded={setEnded}
        />
      </Shell>
    )
  }

  const back = () => { setAccount(null); setCodeSignIn(false); setProvenPassword('') }

  if (account) {
    return (
      <Shell narrow title={ACCOUNT_STEPS[account].title}>
        <AccountScreen
          step={account} providerName={pool ?? undefined} username={username} onUsernameChange={setUsername}
          onStep={setAccount} onBack={back} onEnded={setEnded} links={page.links} initialPassword={provenPassword}
        />
      </Shell>
    )
  }

  if (codeSignIn) {
    return (
      <Shell narrow title="Sign in with a code">
        <OTPScreen username={username} onUsernameChange={setUsername} flowID={page.flow_id} onBack={back} onEnded={setEnded} />
      </Shell>
    )
  }

  // A lone pool's form needs no choosing, so pools are offered as buttons only when there are several.
  const pools = options.pools.length > 1 ? options.pools : []
  const choices = options.redirects.length > 0 || options.otp || pools.length > 0
  const form = pool ? (
    // Keyed by pool, so a password typed for one account is never sent to another.
    <PasswordScreen
      key={pool} providerName={pool} username={username} onUsernameChange={setUsername}
      flowID={page.flow_id} onEnded={setEnded} onForgot={() => setAccount('forgot')}
      onSignUp={() => setAccount('sign-up')} onUnconfirmed={proven => { setProvenPassword(proven); setAccount('verify') }}
    />
  ) : null

  const shell = { narrow: !(choices && form), client: clientLabel(page.client.name), title: null }
  // The choices are gone while a redirect is under way, so nothing on the page can start a second sign-in.
  if (leaving.option) {
    return <Shell {...shell}><LeavingScreen option={leaving.option} height={leaving.height} /></Shell>
  }

  return (
    <Shell {...shell}>
      <div ref={body}>
        <h1>Sign in</h1>
        <div class={choices && form ? 'columns' : ''}>
          {choices ? (
            <div class="methods">
              {leaving.stalled ? <Assist error={STALLED} /> : null}
              {options.redirects.map(r => <RedirectButton key={r.id} option={r} onLeave={o => leaving.leave(o, body.current?.offsetHeight ?? 0)} />)}
              {options.redirects.length && (pools.length || options.otp) ? <div class="or" role="separator">or</div> : null}
              {pools.map(p => <PoolButton key={p.id} pool={p} selected={p.id === pool} onSelect={setPool} />)}
              {options.otp ? <CodeButton onClick={() => setCodeSignIn(true)} /> : null}
            </div>
          ) : null}
          {form}
        </div>
      </div>
    </Shell>
  )
}

/** The card; `narrow` for a single column, which looks abandoned at full width; a null title leaves the heading to the screen; `client` names the app being signed in to. */
function Shell({ children, title = 'Sign in', client = null, narrow = false }: {
  children: ComponentChildren; title?: string | null; client?: string | null; narrow?: boolean
}) {
  return (
    <div class={narrow ? 'frame frame--narrow' : 'frame'}>
      <main class="card">
        <header class="topbar">
          <div class="wordmark"><Wordmark /></div>
          {client ? <ClientPill name={client} /> : null}
        </header>
        {title === null ? null : <h1>{title}</h1>}
        {children}
      </main>
      <PageFooter links={page.links} />
    </div>
  )
}

/** The app being signed in to, kept small beside the wordmark so a long name or a bare client id never outweighs the page; a cut name stays whole for a tooltip and a screen reader. */
function ClientPill({ name }: { name: string }) {
  return (
    <p class="client" title={name}>
      <span class="client-tile" aria-hidden="true"><AppMark /></span>
      <span class="visually-hidden">Signing in to </span>
      <span class="client-name">{name}</span>
    </p>
  )
}
