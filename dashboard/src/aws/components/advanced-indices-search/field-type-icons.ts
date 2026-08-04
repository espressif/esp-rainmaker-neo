/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Check, Hash, Type, type LucideIcon } from "lucide-react";
import type { FieldType } from "./advanced-indices-search.types";

/**
 * Icon per field type. Used wherever a field's type has to read at a glance and
 * no field-specific icon is available — the search bar's type badge, and the
 * fallback for query rules on custom tag fields outside the field catalog.
 */
export const FIELD_TYPE_ICONS: Record<FieldType, LucideIcon> = {
  String: Type,
  Number: Hash,
  Boolean: Check,
};
