/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ThingListFilters } from "../nodes.props";

const SHADOW_ONLINE = "shadow.name.iparams.reported.online";
const SHADOW_DEVICE_TYPE =
  "shadow.name.iparams.reported.data.device.t.type";
const SHADOW_DEVICE_MODEL =
  "shadow.name.iparams.reported.data.device.t.model";
const SHADOW_FW_VERSION =
  "shadow.name.iparams.reported.data.device.t.fw_version";

export function buildThingsSearchQuery(filters: ThingListFilters): string {
  const parts: string[] = [];

  if (filters.advancedSearchQuery) {
    parts.push(filters.advancedSearchQuery);
  }

  if (filters.thingName) {
    parts.push(`thingName:*${filters.thingName}*`);
  }

  if (filters.status === "online") {
    parts.push(`${SHADOW_ONLINE}:true`);
  } else if (filters.status === "offline") {
    parts.push(`${SHADOW_ONLINE}:false`);
  }

  if (filters.typeModel?.type) {
    parts.push(`${SHADOW_DEVICE_TYPE}:${filters.typeModel.type}`);
    if (filters.typeModel.model) {
      parts.push(`${SHADOW_DEVICE_MODEL}:${filters.typeModel.model}`);
    }
  }

  if (filters.fwVersion) {
    parts.push(`${SHADOW_FW_VERSION}:${filters.fwVersion}`);
  }

  return parts.length > 0 ? parts.join(" AND ") : "*";
}
