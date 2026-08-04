/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { SimplifiedDate } from "@espressif/dashboard-ui-components";
import { ThingDeviceAvatar } from "@/components/avatars/thing-device-avatar";
import { OtaJobStatusBadge } from "@/components/ota-job/ota-job-status-badge";
import type { OtaJobNodeExecutionRow } from "../ota-job-node-executions-card.props";

export function getOtaJobNodeExecutionsColumns(
  t: TFunction,
): ColumnDef<OtaJobNodeExecutionRow>[] {
  return [
    {
      accessorKey: "thingName",
      header: t("details.nodes.executions.columns.nodeId", "Node ID"),
      enableHiding: false,
      enableSorting: false,
      cell: ({ row }) => (
        <div className="flex min-w-0 items-center gap-3">
          <ThingDeviceAvatar deviceType={null} online={null} />
          <span className="min-w-0 truncate text-sm font-semibold leading-tight">
            {row.original.thingName}
          </span>
        </div>
      ),
    },
    {
      accessorKey: "status",
      header: t("common:columns.status", "Status"),
      enableSorting: false,
      cell: ({ row }) => <OtaJobStatusBadge status={row.original.status} />,
    },
    {
      accessorKey: "lastUpdatedAt",
      header: t(
        "common:columns.lastUpdatedAt",
        "Last Updated At",
      ),
      enableSorting: false,
      cell: ({ row }) => (
        <SimplifiedDate relative ts={row.original.lastUpdatedAt?.getTime()} />
      ),
    },
  ];
}
