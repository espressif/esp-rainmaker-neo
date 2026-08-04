/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface RedirectUrisCardProps {
  /** Card header label, e.g. "Redirect URIs". */
  title: string;
  /** Redirect URIs to list; each is rendered as an external primary Link. */
  uris: string[];
}
