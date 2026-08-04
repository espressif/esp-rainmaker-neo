/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { IdTokenClaims } from "@/lib/auth";

export interface ProfileDetailsCardProps {
  /** Decoded id_token claims; null when the token is missing or undecodable. */
  claims: IdTokenClaims | null;
}
