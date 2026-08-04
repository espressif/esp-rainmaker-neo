/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ManageThingTagsType } from "../manage-thing-tags.config";
import type { TagRow } from "../manage-thing-tags.utils";

export interface ManageThingTagsRowActionsProps {
  thingName: string;
  type: ManageThingTagsType;
  row: TagRow;
}
