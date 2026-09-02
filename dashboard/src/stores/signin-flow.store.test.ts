/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { beforeEach, describe, expect, it } from 'vitest'
import { useSigninFlowStore } from './signin-flow.store'
import { INITIAL_SIGNIN_STATE } from '../pages/login/_utils/signin-flow'

const EXPIRED: { key: string; fallback: string } = {
  key: 'common:otpErrors.sessionExpired',
  fallback: 'That sign-in attempt timed out. Enter your email to start again.',
}

// The store is a module-level singleton, so every test starts from a clean slate.
beforeEach(() => {
  useSigninFlowStore.setState({
    ...INITIAL_SIGNIN_STATE,
    keepSignedIn: false,
    flowMessage: null,
  })
})

describe('signin-flow store', () => {
  it('routes events through the reducer', () => {
    const { dispatch } = useSigninFlowStore.getState()

    dispatch({ type: 'identified', username: 'admin@example.com' })
    dispatch({
      type: 'challenges',
      session: 's-1',
      challenges: ['EMAIL_OTP', 'PASSWORD'],
    })

    const state = useSigninFlowStore.getState()
    expect(state.step).toBe('otp')
    expect(state.username).toBe('admin@example.com')
    expect(state.session).toBe('s-1')
    expect(state.challenges).toEqual(['EMAIL_OTP', 'PASSWORD'])
  })

  it('rotates the session on every otpSent', () => {
    const { dispatch } = useSigninFlowStore.getState()

    dispatch({ type: 'identified', username: 'admin@example.com' })
    dispatch({ type: 'otpSent', session: 's-1', destination: 'a***@e***.com' })
    dispatch({ type: 'otpSent', session: 's-2', destination: 'a***@e***.com' })

    expect(useSigninFlowStore.getState().session).toBe('s-2')
  })

  it('returns to the initial flow state on restart', () => {
    const { dispatch } = useSigninFlowStore.getState()

    dispatch({ type: 'identified', username: 'admin@example.com' })
    dispatch({ type: 'otpSent', session: 's-1', destination: 'a***@e***.com' })
    dispatch({ type: 'restart' })

    const state = useSigninFlowStore.getState()
    expect(state.step).toBe(INITIAL_SIGNIN_STATE.step)
    expect(state.username).toBe('')
    expect(state.session).toBeNull()
    expect(state.challenges).toEqual([])
    expect(state.destination).toBeNull()
  })

  it('keeps flowMessage and keepSignedIn across reducer events', () => {
    const store = useSigninFlowStore.getState()
    store.setKeepSignedIn(true)
    store.setFlowMessage(EXPIRED)

    store.dispatch({ type: 'identified', username: 'admin@example.com' })
    store.dispatch({ type: 'sessionExpired' })
    store.dispatch({ type: 'restart' })

    const state = useSigninFlowStore.getState()
    expect(state.keepSignedIn).toBe(true)
    expect(state.flowMessage).toEqual(EXPIRED)
  })

  it('clears flow state and flowMessage on reset, keeping the preference', () => {
    const store = useSigninFlowStore.getState()
    store.setKeepSignedIn(true)
    store.setFlowMessage(EXPIRED)
    store.dispatch({ type: 'identified', username: 'admin@example.com' })

    useSigninFlowStore.getState().reset()

    const state = useSigninFlowStore.getState()
    expect(state.username).toBe('')
    expect(state.session).toBeNull()
    expect(state.flowMessage).toBeNull()
    expect(state.keepSignedIn).toBe(true)
  })
})
