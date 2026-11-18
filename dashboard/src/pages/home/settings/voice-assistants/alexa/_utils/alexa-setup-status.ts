/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { AlexaSetupStatus } from "@/api/integrations";

/** The worker runs in a Lambda capped at 15 min and republishes on every stage, so an IN_PROGRESS status older than this (plus clock-skew margin) belongs to a worker that died. */
export const SETUP_STALL_MS = 20 * 60 * 1000;

/** True for an IN_PROGRESS status nothing has updated within SETUP_STALL_MS of nowMs. */
export function isSetupStalled(status: AlexaSetupStatus | undefined, nowMs: number): boolean {
  return status?.status === "IN_PROGRESS" && nowMs - Date.parse(status.updated_at) > SETUP_STALL_MS;
}

/** The skill a live run is configuring, so a page opened mid-run (or reloaded) resumes polling: IN_PROGRESS and not stalled -> its ref_id, else undefined. */
export function runningSetupSkillId(status: AlexaSetupStatus | undefined, nowMs: number): string | undefined {
  return status?.status === "IN_PROGRESS" && !isSetupStalled(status, nowMs) ? status.ref_id : undefined;
}

/** The last run if it failed or stalled, or undefined while a newer run is pending, since that run supersedes it. */
export function failedSetup(
  status: AlexaSetupStatus | undefined,
  pendingSkillId: string | undefined,
  nowMs: number,
): AlexaSetupStatus | undefined {
  if (pendingSkillId) {
    return undefined;
  }
  return status?.status === "FAILED" || isSetupStalled(status, nowMs) ? status : undefined;
}

/** True once the run for skillId has ended or stalled. A status for another skill is a previous run's and does not count. */
export function isSetupFinished(status: AlexaSetupStatus | undefined, skillId: string, nowMs: number): boolean {
  if (status?.ref_id !== skillId) {
    return false;
  }
  return status.status === "DONE" || status.status === "FAILED" || isSetupStalled(status, nowMs);
}
