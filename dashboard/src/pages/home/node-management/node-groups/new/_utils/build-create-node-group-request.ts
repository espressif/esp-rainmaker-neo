/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { buildQueryFromRules } from "@/aws/components/query-rule-builder/query-rule-builder.utils";
import type { CreateNodeGroupRequest } from "@/aws/services/thing-group.service";
import type { CreateNodeGroupFormValues } from "../_schema/create-node-group-form.schema";

export function buildCreateNodeGroupRequest(
  values: CreateNodeGroupFormValues,
): CreateNodeGroupRequest {
  const description = values.description?.trim() || undefined;

  if (values.createAsDynamic) {
    return {
      kind: "dynamic",
      thingGroupName: values.groupName,
      description,
      queryString: buildQueryFromRules(values.queryRules),
    };
  }

  const parentGroupName =
    values.createAsSubgroup && values.parentGroupName.trim()
      ? values.parentGroupName.trim()
      : undefined;

  return {
    kind: "static",
    thingGroupName: values.groupName,
    description,
    ...(parentGroupName && { parentGroupName }),
  };
}
