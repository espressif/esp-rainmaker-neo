/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { getIdTokenClaims } from "@/lib/auth";
import type { IdTokenClaims } from "@/lib/auth";

/**
 * Decoded id_token claims for the signed-in admin. Read once per mount —
 * the token only changes on login/refresh, which remounts the app.
 */
export function useProfileClaims(): IdTokenClaims | null {
  return useMemo(() => getIdTokenClaims(), []);
}
