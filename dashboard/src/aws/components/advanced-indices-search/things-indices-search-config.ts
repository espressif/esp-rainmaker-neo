/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  Cpu,
  DoorOpen,
  FileCode,
  MapPin,
  Package,
  Smartphone,
  Terminal,
  UserCircle,
  Wifi,
  type LucideIcon,
} from "lucide-react";
import type { FieldType } from "./advanced-indices-search.types";

export interface AdvancedSearchFieldDef {
  name: string;
  type: FieldType;
  /** English fallback for `labelKey`. */
  label: string;
  /** Fully-qualified i18n key, so consumers bound to any namespace can resolve it. */
  labelKey: string;
  /**
   * Icon representing what the field is about. Kept as a component (not an
   * element) so each consumer sizes it for its own surface. Fields without one
   * fall back to `FIELD_TYPE_ICONS`.
   */
  icon?: LucideIcon;
}

export const advancedSearchFieldsData: AdvancedSearchFieldDef[] = [
  {
    name: "shadow.name.iparams.reported.data.device.t.type",
    type: "String",
    label: "Device Type",
    labelKey: "common:searchFields.deviceType",
    icon: Cpu,
  },
  {
    name: "shadow.name.iparams.reported.data.device.t.model",
    type: "String",
    label: "Device Model",
    labelKey: "common:searchFields.deviceModel",
    icon: Smartphone,
  },
  {
    name: "shadow.name.iparams.reported.data.device.t.fw_version",
    type: "String",
    label: "Firmware Version",
    labelKey: "common:searchFields.firmwareVersion",
    icon: FileCode,
  },
  {
    name: "shadow.name.iparams.reported.online",
    type: "Boolean",
    label: "Online Status",
    labelKey: "common:searchFields.onlineStatus",
    icon: Wifi,
  },
  {
    name: "shadow.name.iparams.reported.data.admin.t.created_by",
    type: "String",
    label: "Created By",
    labelKey: "common:searchFields.createdBy",
    icon: UserCircle,
  },
  {
    name: "shadow.name.iparams.reported.data.admin.t.registered_from",
    type: "String",
    label: "Registered From",
    labelKey: "common:searchFields.registeredFrom",
    icon: Terminal,
  },
  {
    name: "shadow.name.iparams.reported.data.admin.t.batch",
    type: "String",
    label: "Registration Batch",
    labelKey: "common:searchFields.registrationBatch",
    icon: Package,
  },
  {
    name: "shadow.name.iparams.reported.data.user.t.room",
    type: "String",
    label: "Room",
    labelKey: "common:searchFields.room",
    icon: DoorOpen,
  },
  {
    name: "shadow.name.iparams.reported.data.user.t.location",
    type: "String",
    label: "User Location",
    labelKey: "common:searchFields.userLocation",
    icon: MapPin,
  },
];
