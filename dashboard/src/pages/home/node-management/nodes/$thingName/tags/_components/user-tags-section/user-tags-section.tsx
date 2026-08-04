/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import ManageThingTags from "../manage-thing-tags";

interface UserTagsSectionProps {
  thingName: string;
}

export default function UserTagsSection({ thingName }: UserTagsSectionProps) {
  return <ManageThingTags thingName={thingName} type="user" />;
}
