/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from "vitest";
import { changePasswordRequestFor, passwordModeFor } from "./password-factor";

describe("passwordModeFor", () => {
  it("is a change when the admin already has a password", () => {
    expect(passwordModeFor({ hasPassword: true })).toBe("change");
  });

  it("is a first-time set when the admin has none", () => {
    expect(passwordModeFor({ hasPassword: false })).toBe("set");
  });

  it("assumes a password while the factors are still unknown", () => {
    // Rendering the two-field "set" form to someone who has a password would send
    // ChangePassword without PreviousPassword and be rejected, so undefined is
    // resolved the safe way round.
    expect(passwordModeFor(undefined)).toBe("change");
  });
});

describe("changePasswordRequestFor", () => {
  const values = {
    old_password: "OldPass1!",
    new_password: "NewPass1!",
    confirm_password: "NewPass1!",
  };

  it("carries the previous password when changing", () => {
    expect(changePasswordRequestFor("change", "tok", values)).toEqual({
      access_token: "tok",
      old_password: "OldPass1!",
      new_password: "NewPass1!",
    });
  });

  it("omits the previous password entirely when setting a first one", () => {
    // Cognito rejects an empty-string PreviousPassword; the key must be absent.
    const request = changePasswordRequestFor("set", "tok", values);
    expect(request).toEqual({ access_token: "tok", new_password: "NewPass1!" });
    expect("old_password" in request).toBe(false);
  });

  it("omits it even when the field still holds a stale value", () => {
    const request = changePasswordRequestFor("set", "tok", {
      ...values,
      old_password: "leftover",
    });
    expect("old_password" in request).toBe(false);
  });
});
