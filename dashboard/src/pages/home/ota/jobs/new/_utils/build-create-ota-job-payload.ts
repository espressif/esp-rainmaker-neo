/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { CreateOtaJobParams } from "@/aws/services/ota.service";
import { getOtaS3Bucket, getOtaServiceRoleArn } from "@/lib/config";
import {
  JOB_MODE_CONTINUOUS,
  SOURCE_RULES,
  TARGET_TYPE_GROUP,
} from "../_constants/create-ota-job-form.constants";
import type {
  CreateOtaJobFormValues,
  CreateOtaJobQueryRule,
} from "../_schema/create-ota-job-form.schema";

/** Build an IoT fleet-index query string (`field:value AND …`) from rules. */
function buildQueryFromRules(rules: CreateOtaJobQueryRule[]): string {
  return rules
    .filter((rule) => rule.field && rule.value)
    .map((rule) => `${rule.field}:${rule.value}`)
    .join(" AND ");
}

/**
 * Map validated form values to the `createOTAJob` service params. A dynamic
 * group query is produced only for continuous group rollouts sourced from
 * rules (mirrors the `showRules` condition in the target-details section);
 * every other case targets the selected group/node by name.
 */
export function buildCreateOtaJobPayload(
  values: CreateOtaJobFormValues,
  fwVersion?: string,
): CreateOtaJobParams {
  const useRules =
    values.targetType === TARGET_TYPE_GROUP &&
    values.targetSelection === JOB_MODE_CONTINUOUS &&
    values.source === SOURCE_RULES &&
    values.queryRules.length > 0;

  const dynamicGroupQuery = useRules
    ? buildQueryFromRules(values.queryRules)
    : undefined;

  return {
    otaUpdateId: values.name,
    targetType: values.targetType,
    targetSelection: values.targetSelection,
    targetName: values.targetName,
    dynamicGroupQuery,
    firmwareKey: values.firmwareKey,
    roleArn: getOtaServiceRoleArn(),
    bucket: getOtaS3Bucket(),
    fwVersion,
    fileMd5: values.fileMd5 || undefined,
  };
}
