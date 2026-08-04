/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useNavigate } from "@tanstack/react-router";
import { Group } from "lucide-react";
import {
  Alert,
  SimpleClickableCard,
} from "@espressif/dashboard-ui-components/components";
import type { GroupListProps } from "./group-list.props";

export function GroupList({ groupNames, emptyText }: GroupListProps) {
  const navigate = useNavigate();

  if (groupNames.length === 0) {
    return (
      <Alert variant="soft" color="info" type="info" hideIcon>
        {emptyText}
      </Alert>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      {groupNames.map((groupName) => (
        <SimpleClickableCard
          key={groupName}
          color="mist"
          variant="soft"
          size="sm"
          icon={<Group />}
          title={groupName}
          onClick={() =>
            void navigate({
              to: "/home/node-management/node-groups/$groupName",
              params: { groupName },
            })
          }
        />
      ))}
    </div>
  );
}
