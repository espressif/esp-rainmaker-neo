/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface LanguageCardProps {
  /** Opens the language picker. The card owns no container, so the page decides how. */
  onChangeLanguage: () => void;
}
