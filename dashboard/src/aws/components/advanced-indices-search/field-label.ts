/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import type { IndexFieldRef } from "./advanced-indices-search.types";

/**
 * Display label for a searchable field. Catalog fields carry a fully-qualified
 * `labelKey`, so this resolves regardless of which namespace the caller's `t` is
 * bound to. Custom tag fields typed into the search bar have neither key nor
 * label and fall back to the raw field path.
 */
export function fieldLabel(field: IndexFieldRef, t: TFunction): string {
  if (field.labelKey) {return t(field.labelKey, field.label ?? field.name);}
  return field.label ?? field.name;
}
