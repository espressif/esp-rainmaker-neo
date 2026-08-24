/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from "vitest";
import {
  INITIAL_SIGNIN_STATE,
  shouldAutoSendOtp,
  signinReducer,
  type SigninState,
} from "./signin-flow";

function advance(events: Parameters<typeof signinReducer>[1][]): SigninState {
  return events.reduce(signinReducer, INITIAL_SIGNIN_STATE);
}

describe("signinReducer", () => {
  it("starts by asking for an address", () => {
    expect(INITIAL_SIGNIN_STATE.step).toBe("identify");
  });

  it("goes straight to the code screen when EMAIL_OTP is the only choice", () => {
    const state = advance([
      { type: "identified", username: "a@esp.com" },
      { type: "challenges", session: "s1", challenges: ["EMAIL_OTP"] },
    ]);
    expect(state.step).toBe("otp");
    expect(state.session).toBe("s1");
    expect(state.username).toBe("a@esp.com");
  });

  it("still goes to the code screen when the admin also has a password", () => {
    const state = advance([
      { type: "identified", username: "a@esp.com" },
      { type: "challenges", session: "s1", challenges: ["EMAIL_OTP", "PASSWORD"] },
    ]);
    expect(state.step).toBe("otp");
    expect(state.challenges).toEqual(["EMAIL_OTP", "PASSWORD"]);
  });

  it("goes straight to the password form when PASSWORD is the only choice", () => {
    const state = advance([
      { type: "identified", username: "a@esp.com" },
      { type: "challenges", session: "s1", challenges: ["PASSWORD"] },
    ]);
    expect(state.step).toBe("password");
  });

  it("records the masked destination when a code is sent", () => {
    const state = advance([
      { type: "identified", username: "a@esp.com" },
      { type: "challenges", session: "s1", challenges: ["EMAIL_OTP", "PASSWORD"] },
      { type: "otpSent", session: "s2", destination: "a***@e***.com" },
    ]);
    expect(state.step).toBe("otp");
    expect(state.session).toBe("s2");
    expect(state.destination).toBe("a***@e***.com");
  });

  it("falls back to the password form when the client rejects USER_AUTH", () => {
    const state = advance([
      { type: "identified", username: "a@esp.com" },
      { type: "userAuthUnavailable" },
    ]);
    expect(state.step).toBe("password");
    expect(state.username).toBe("a@esp.com");
  });

  it("keeps the username but drops the session when it expires", () => {
    const state = advance([
      { type: "identified", username: "a@esp.com" },
      { type: "challenges", session: "s1", challenges: ["EMAIL_OTP"] },
      { type: "sessionExpired" },
    ]);
    expect(state.step).toBe("identify");
    expect(state.session).toBeNull();
    expect(state.username).toBe("a@esp.com");
  });

  it("returns to the choice screen from the code screen when both factors exist", () => {
    const state = advance([
      { type: "identified", username: "a@esp.com" },
      { type: "challenges", session: "s1", challenges: ["EMAIL_OTP", "PASSWORD"] },
      { type: "otpSent", session: "s2", destination: "a***@e***.com" },
      { type: "usePassword" },
    ]);
    expect(state.step).toBe("password");
  });

  it("resets to identify when the admin edits the address", () => {
    const state = advance([
      { type: "identified", username: "a@esp.com" },
      { type: "challenges", session: "s1", challenges: ["EMAIL_OTP"] },
      { type: "restart" },
    ]);
    expect(state).toEqual(INITIAL_SIGNIN_STATE);
  });

  it("ignores a challenges event that arrives before an address", () => {
    const state = signinReducer(INITIAL_SIGNIN_STATE, {
      type: "challenges",
      session: "s1",
      challenges: ["EMAIL_OTP"],
    });
    expect(state).toEqual(INITIAL_SIGNIN_STATE);
  });

  it("ignores an empty challenge list rather than stranding the admin", () => {
    const state = advance([
      { type: "identified", username: "a@esp.com" },
      { type: "challenges", session: "s1", challenges: [] },
    ]);
    expect(state.step).toBe("password");
  });
});

describe("shouldAutoSendOtp", () => {
  it("auto-sends when EMAIL_OTP is the admin's only factor", () => {
    expect(shouldAutoSendOtp(["EMAIL_OTP"])).toBe(true);
  });

  it("auto-sends even when a password is offered too, since the code is the default", () => {
    expect(shouldAutoSendOtp(["EMAIL_OTP", "PASSWORD"])).toBe(true);
  });

  it("does not auto-send when PASSWORD is the only factor", () => {
    expect(shouldAutoSendOtp(["PASSWORD"])).toBe(false);
  });

  it("does not auto-send when Cognito offered nothing this app can drive", () => {
    expect(shouldAutoSendOtp([])).toBe(false);
  });
});
