/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { create } from 'zustand'
// Relative imports (not `@/…`) so the alias-free `vitest.config.ts` can load this
// module: the store is unit-tested in a plain node environment.
import {
  INITIAL_SIGNIN_STATE,
  signinReducer,
  type SigninEvent,
  type SigninState,
} from '../pages/login/_utils/signin-flow'
import { getKeepSignedIn } from '../lib/auth/auth.storage'
import type { LocalizedMessage } from '../lib/auth/password-errors'

/**
 * The sign-in flow's cross-route state. Each step of the flow is its own route, so
 * the Cognito auth session, challenge list and address have to outlive any single
 * page — but deliberately not the tab: nothing here is persisted. A refresh drops
 * the store, the step guards notice the missing state and bounce to the entry
 * screen, and no credential ever touches localStorage from this flow.
 */
interface SigninFlowStore extends SigninState {
  /**
   * Whether the upcoming sign-in should persist the refresh token. Lives here (not
   * in page state) because the checkbox appears on two different routes and the
   * choice must survive moving between them. Prefilled from the last sign-in;
   * written back to storage only by a successful completion.
   */
  keepSignedIn: boolean
  /**
   * A notice that must survive a redirect — e.g. "session expired" set just before
   * the step guard bounces to the entry screen. Mutation errors are page-local and
   * die with their page; this is the one message that cannot.
   */
  flowMessage: LocalizedMessage | null
  /** Advance the flow via the pure reducer (see `signin-flow.ts`). */
  dispatch: (event: SigninEvent) => void
  setKeepSignedIn: (value: boolean) => void
  setFlowMessage: (message: LocalizedMessage | null) => void
  /** Full reset after a completed sign-in: flow state and notice, preference kept. */
  reset: () => void
}

export const useSigninFlowStore = create<SigninFlowStore>()((set) => ({
  ...INITIAL_SIGNIN_STATE,
  keepSignedIn: getKeepSignedIn(),
  flowMessage: null,

  dispatch: (event) =>
    set((state) =>
      signinReducer(
        {
          step: state.step,
          username: state.username,
          session: state.session,
          challenges: state.challenges,
          destination: state.destination,
        },
        event,
      ),
    ),

  setKeepSignedIn: (value) => set({ keepSignedIn: value }),

  setFlowMessage: (message) => set({ flowMessage: message }),

  reset: () => set({ ...INITIAL_SIGNIN_STATE, flowMessage: null }),
}))
