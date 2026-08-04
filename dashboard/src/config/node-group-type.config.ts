/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export type NodeGroupType = "static" | "dynamic";

/**
 * `queryString` is only ever populated for dynamic groups, and it is required on
 * CreateDynamicThingGroup — so it can never be absent on one. AWS does not allow converting
 * between static and dynamic, which makes this discriminator stable for the life of the group.
 */
export function isDynamicNodeGroup(
  queryString: string | null | undefined,
): boolean {
  return !!queryString;
}

export function getNodeGroupType(
  queryString: string | null | undefined,
): NodeGroupType {
  return isDynamicNodeGroup(queryString) ? "dynamic" : "static";
}
