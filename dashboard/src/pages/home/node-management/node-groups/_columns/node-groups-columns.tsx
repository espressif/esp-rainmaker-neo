/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { CopiableText } from "@espressif/dashboard-ui-components/components";
import { NodeGroupAvatar } from "@/components/avatars/node-group-avatar";
import { DescriptionCell } from "../_components/description-cell";
import { NodeGroupsRowActions } from "../_components/node-groups-row-actions";
import { ParentGroupsPopover } from "../_components/parent-groups-popover";
import type { NodeGroupRow } from "../node-groups.props";

export function getNodeGroupsColumns(t: TFunction): ColumnDef<NodeGroupRow>[] {
  return [
    {
      accessorKey: "groupName",
      header: t("common:columns.nameId", "Name / ID"),
      enableHiding: false,
      cell: ({ row }) => {
        const group = row.original;
        return (
          <div className="flex min-w-0 items-center gap-3">
            <NodeGroupAvatar />
            <div className="min-w-0 flex flex-col">
              <p className="text-sm font-semibold truncate leading-tight">
                {group.groupName}
              </p>
              {group.groupId ? (
                <CopiableText
                  text={group.groupId}
                  className="text-xs text-muted-foreground truncate leading-tight"
                />
              ) : null}

              <ParentGroupsPopover
                parentGroupNames={row.original.parentGroupNames}
              />
            </div>
          </div>
        );
      },
    },
    {
      accessorKey: "groupDescription",
      header: t("columns.description", "Description"),
      cell: ({ row }) => (
        <DescriptionCell description={row.original.groupDescription} />
      ),
    },
    {
      id: "actions",
      header: "",
      enableSorting: false,
      enableHiding: false,
      size: 120,
      cell: ({ row }) => (
        <div className="flex justify-end">
          <NodeGroupsRowActions groupName={row.original.groupName} />
        </div>
      ),
    },
  ];
}
