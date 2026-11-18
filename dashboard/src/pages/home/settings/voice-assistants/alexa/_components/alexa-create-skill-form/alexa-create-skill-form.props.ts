/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface AlexaCreateSkillFormProps {
  /**
   * Invoked when the user cancels. The host (sheet / dialog / page) decides what
   * closing means, keeping this form decoupled from its container.
   */
  onCancel?: () => void;
  /**
   * Invoked with the new skill id once the backend accepts the request. The skill is not
   * configured yet at this point, so hosts should treat it as "started", not "done".
   */
  onCreated?: (skillId: string) => void;
}
