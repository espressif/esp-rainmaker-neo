/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export type ThingOnlineStatus = "online" | "offline";

export type ThingTypeModelFilterValue = {
  type: string;
  model?: string;
};

export interface ThingListFilters {
  thingName?: string;
  status?: ThingOnlineStatus;
  typeModel?: ThingTypeModelFilterValue;
  fwVersion?: string;
  advancedSearchQuery?: string;
}

export const CLEARED_THING_LIST_FILTERS: ThingListFilters = {
  thingName: undefined,
  status: undefined,
  typeModel: undefined,
  fwVersion: undefined,
  advancedSearchQuery: undefined,
};

export function hasActiveThingFilters(filters: ThingListFilters): boolean {
  if (filters.thingName) {
    return true;
  }
  if (filters.status) {
    return true;
  }
  if (filters.typeModel) {
    return true;
  }
  if (filters.fwVersion) {
    return true;
  }
  if (filters.advancedSearchQuery) {
    return true;
  }
  return false;
}
