/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import ManageThingTags from "../manage-thing-tags";

interface DeviceTagsSectionProps {
  thingName: string;
}

export default function DeviceTagsSection({
  thingName,
}: DeviceTagsSectionProps) {
  return <ManageThingTags thingName={thingName} type="device" readOnly />;
}
