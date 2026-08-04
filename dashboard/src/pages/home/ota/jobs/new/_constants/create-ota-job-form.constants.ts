/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** Target type card ids (Node group vs Node). */
export const TARGET_TYPE_GROUP = "group";
export const TARGET_TYPE_NODE = "node";

/** Continuous switch-card id. */
export const CONTINUOUS_CARD_ID = "continuous";

/** Target source card ids (multi-select). */
export const SOURCE_EXISTING = "existing";
export const SOURCE_RULES = "rules";

/** Job mode values persisted on the form. */
export const JOB_MODE_SNAPSHOT = "SNAPSHOT";
export const JOB_MODE_CONTINUOUS = "CONTINUOUS";

/** Name may contain letters, numbers, underscores and hyphens only. */
export const OTA_JOB_NAME_REGEX = /^[a-zA-Z0-9_-]+$/;

/** Max length accepted for the OTA job name. */
export const OTA_JOB_NAME_MAX_LENGTH = 128;

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
