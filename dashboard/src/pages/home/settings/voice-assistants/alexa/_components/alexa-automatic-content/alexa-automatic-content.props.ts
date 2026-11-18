/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { AlexaConfigGetResponse, AlexaSetupStatus } from "@/api/integrations";

export interface AlexaAutomaticContentProps {
  /** Fetched Alexa configuration; `undefined` while loading or when absent. */
  data: AlexaConfigGetResponse | undefined;
  isLoading: boolean;
  error: Error | null;
  /** Opens the create-skill form. */
  onCreateSkill: () => void;
  /** Skill id whose background configuration we are still waiting on, if any. */
  pendingSkillId?: string;
  /** The stage that run last reported, e.g. "manifest build". */
  pendingStage?: string;
  /** Last automatic setup's status when it failed or stalled; absent while a run is pending. */
  failedStatus?: AlexaSetupStatus;
}
