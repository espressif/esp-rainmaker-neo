/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import ManageThingTags from "../manage-thing-tags";

interface AdminTagsSectionProps {
  thingName: string;
}

export default function AdminTagsSection({ thingName }: AdminTagsSectionProps) {
  return <ManageThingTags thingName={thingName} type="admin" />;
}
