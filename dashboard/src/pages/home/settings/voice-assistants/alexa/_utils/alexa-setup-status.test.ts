/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from "vitest";
import type { AlexaSetupState, AlexaSetupStatus } from "@/api/integrations";
import {
  SETUP_STALL_MS,
  failedSetup,
  isSetupFinished,
  isSetupStalled,
  runningSetupSkillId,
} from "./alexa-setup-status";

const UPDATED_AT = "2026-10-26T00:00:00Z";
const FRESH = Date.parse(UPDATED_AT) + 60_000;
const STALE = Date.parse(UPDATED_AT) + SETUP_STALL_MS + 1;

const doc = (status: string, refId: string): AlexaSetupStatus => ({ status: status as AlexaSetupState, ref_id: refId, updated_at: UPDATED_AT });

describe("isSetupStalled", () => {
  it.each([
    ["IN_PROGRESS", FRESH, false],
    ["IN_PROGRESS", STALE, true],
    ["DONE", STALE, false],
  ])("%s at %d -> %s", (status, now, want) => {
    expect(isSetupStalled(doc(status, "skill-a"), now)).toBe(want);
  });
});

describe("isSetupFinished", () => {
  it.each([
    ["IN_PROGRESS", "skill-a", FRESH, false],
    ["IN_PROGRESS", "skill-a", STALE, true],
    ["DONE", "skill-a", FRESH, true],
    ["FAILED", "skill-a", FRESH, true],
    ["DONE", "skill-old", FRESH, false],
  ])("%s for %s -> %s", (status, refId, now, want) => {
    expect(isSetupFinished(doc(status, refId), "skill-a", now)).toBe(want);
  });
});

describe("failedSetup", () => {
  const failed = doc("FAILED", "skill-a");
  const running = doc("IN_PROGRESS", "skill-a");

  it("reports the last failure when nothing is pending", () => {
    expect(failedSetup(failed, undefined, FRESH)).toBe(failed);
  });

  it("hides it while a newer run is pending", () => {
    expect(failedSetup(failed, "skill-b", FRESH)).toBeUndefined();
  });

  it("reports a stalled run, not a live one", () => {
    expect(failedSetup(running, undefined, STALE)).toBe(running);
    expect(failedSetup(running, undefined, FRESH)).toBeUndefined();
  });
});

describe("runningSetupSkillId", () => {
  it.each([
    ["IN_PROGRESS", FRESH, "skill-a"],
    ["IN_PROGRESS", STALE, undefined],
    ["DONE", FRESH, undefined],
    ["FAILED", FRESH, undefined],
  ])("%s at %d -> %s", (status, now, want) => {
    expect(runningSetupSkillId(doc(status, "skill-a"), now)).toBe(want);
  });
});
