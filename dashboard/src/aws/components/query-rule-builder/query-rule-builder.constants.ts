/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Rule fields whose values can be fetched from the fleet-index buckets
 * aggregation endpoint (registered as custom aggregatable fields). Other
 * string fields fall back to free-text entry.
 */
export const AGGREGATABLE_RULE_FIELDS = new Set<string>([
  "shadow.name.iparams.reported.data.device.t.type",
  "shadow.name.iparams.reported.data.device.t.model",
  "shadow.name.iparams.reported.data.device.t.fw_version",
]);
