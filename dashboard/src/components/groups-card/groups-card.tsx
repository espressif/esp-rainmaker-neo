/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Group } from "lucide-react";
import { SectionCard } from "@espressif/dashboard-ui-components/components";
import { GroupList } from "./group-list";
import type { GroupsCardProps } from "./groups-card.props";

export function GroupsCard({
  groupNames,
  primaryText,
  emptyText,
}: GroupsCardProps) {
  return (
    <SectionCard
      icon={<Group className="h-6 w-6" />}
      primaryText={primaryText}
      color="silver"
      variant="outline"
    >
      <GroupList groupNames={groupNames} emptyText={emptyText} />
    </SectionCard>
  );
}
