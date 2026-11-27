/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * The login page's state machine, kept as a pure reducer with no imports.
 *
 * Two generations of admin coexist permanently: those seeded before passwordless
 * sign-in have a password *and* an email factor, those seeded after have only the
 * email factor. Rather than guess, the page asks Cognito which factors the address
 * actually has and renders exactly those — which is what `challenges` carries.
 */

import type { ChallengeKind } from "@/api";

export type SigninStep = "identify" | "otp" | "password";

export interface SigninState {
  step: SigninStep;
  /** The address the admin typed; retained across steps so it can be resubmitted. */
  username: string;
  /** Cognito's auth session, rotated by every challenge response. */
  session: string | null;
  challenges: ChallengeKind[];
  /** Cognito's masked delivery destination, e.g. `a***@e***.com`. */
  destination: string | null;
  /** Whether a code has been emailed in this attempt; none is sent until the admin asks. */
  codeSent: boolean;
}

export type SigninEvent =
  | { type: "identified"; username: string }
  | { type: "challenges"; session: string; challenges: ChallengeKind[] }
  | { type: "otpSent"; session: string; destination: string }
  | { type: "usePassword" }
  | { type: "userAuthUnavailable" }
  | { type: "sessionExpired" }
  | { type: "restart" };

export const INITIAL_SIGNIN_STATE: SigninState = {
  step: "identify",
  username: "",
  session: null,
  challenges: [],
  destination: null,
  codeSent: false,
};

/** Whether Cognito offers this admin the emailed code. */
export function offersOtp(challenges: ChallengeKind[]): boolean {
  return challenges.includes("EMAIL_OTP");
}

export function signinReducer(
  state: SigninState,
  event: SigninEvent,
): SigninState {
  switch (event.type) {
    case "identified":
      return { ...INITIAL_SIGNIN_STATE, username: event.username };

    case "challenges": {
      // Nothing to select against without an address — this can only be a stale
      // response from a flow the admin has already restarted.
      if (!state.username) {
        return state;
      }
      const challenges = event.challenges;
      // Without the email factor the password form is the one path left, also for an empty list.
      const step: SigninStep = offersOtp(challenges) ? "otp" : "password";
      return { ...state, step, session: event.session, challenges };
    }

    case "otpSent":
      return {
        ...state,
        step: "otp",
        session: event.session,
        destination: event.destination,
        codeSent: true,
      };

    case "usePassword":
      return { ...state, step: "password" };

    // The app client predates ALLOW_USER_AUTH, so the stack update has not landed yet.
    // Falling back keeps the dashboard working against either version of the stack.
    case "userAuthUnavailable":
      return { ...state, step: "password", challenges: ["PASSWORD"] };

    // The session is single-use and short-lived; the address is still good, so only
    // the session is discarded.
    case "sessionExpired":
      return { ...INITIAL_SIGNIN_STATE, username: state.username };

    case "restart":
      return INITIAL_SIGNIN_STATE;
  }
}
