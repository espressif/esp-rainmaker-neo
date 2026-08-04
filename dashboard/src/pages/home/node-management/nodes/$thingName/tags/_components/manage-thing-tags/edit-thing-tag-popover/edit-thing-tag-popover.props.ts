/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ManageThingTagsType } from "../manage-thing-tags.config";

export interface EditThingTagPopoverProps {
  thingName: string;
  type: ManageThingTagsType;
  tagKey: string;
  initialValue: string;
}
