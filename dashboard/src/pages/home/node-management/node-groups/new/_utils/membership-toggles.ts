/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { UseFormSetValue } from "react-hook-form";
import type { CreateNodeGroupFormValues } from "../_schema/create-node-group-form.schema";

type SetValue = UseFormSetValue<CreateNodeGroupFormValues>;

/**
 * Enable/disable "Create as sub-group". Sub-group and dynamic membership are
 * mutually exclusive, so enabling one clears and switches off the other.
 */
export function applySubgroupToggle(enabled: boolean, setValue: SetValue) {
  setValue("createAsSubgroup", enabled);
  if (enabled) {
    setValue("createAsDynamic", false);
    setValue("queryRules", []);
  } else {
    setValue("parentGroupName", "");
  }
}

/** Enable/disable "Create as dynamic group" (see {@link applySubgroupToggle}). */
export function applyDynamicToggle(enabled: boolean, setValue: SetValue) {
  setValue("createAsDynamic", enabled);
  if (enabled) {
    setValue("createAsSubgroup", false);
    setValue("parentGroupName", "");
  } else {
    setValue("queryRules", []);
  }
}
