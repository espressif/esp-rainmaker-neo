/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface AlexaCreateSkillFormSheetProps {
  /** Closes the sheet (mapped from the form's cancel/created callbacks). */
  onClose: () => void;
  /** Forwarded from the form: the new skill id, once the backend accepts the request. */
  onCreated: (skillId: string) => void;
}
