/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { format } from "date-fns";
import { Badge } from "@espressif/dashboard-ui-components/components";
import { Tooltip } from "@espressif/dashboard-ui-components";
import { ThingDeviceAvatar } from "@/components/avatars/thing-device-avatar";
import { ThingNameCell } from "@/aws/components/thing-name-cell/thing-name-cell";
import { ThingStatusBadge } from "@/components/thing/thing-status-badge";
import { TypeModelCell } from "../_components/type-model-cell/type-model-cell";

export interface ThingRow {
  thingId: string;
  thingName: string | null;
  awsThingName: string;
  online: boolean | null;
  deviceType: string | null;
  deviceModel: string | null;
  fwVersion: string | null;
  lastSeen: number | null;
}

function formatLastSeen(ts: number | null): string | null {
  if (!ts) {
    return null;
  }
  return format(new Date(ts * 1000), "dd MMM yyyy, HH:mm");
}

function StatusCell({
  online,
  lastSeen,
  t,
}: {
  online: boolean | null;
  lastSeen: number | null;
  t: TFunction;
}) {
  if (online == null) {
    return <span className="text-muted-foreground">{" "}</span>;
  }

  const badge = <ThingStatusBadge online={online} />;
  const sinceLabel = formatLastSeen(lastSeen);

  if (!sinceLabel) {
    return badge;
  }

  return (
    <Tooltip
      content={t("common:nodeStatus.since", "since {{date}}", {
        date: sinceLabel,
        defaultValue: "since {{date}}",
      })}
    >
      <span className="inline-flex">{badge}</span>
    </Tooltip>
  );
}

export function getNodesColumns(t: TFunction): ColumnDef<ThingRow>[] {
  return [
    {
      accessorKey: "thingId",
      header: t("common:columns.nameId", "Name / ID"),
      enableHiding: false,
      cell: ({ row }) => {
        const thing = row.original;
        return (
          <div className="flex min-w-0 items-center gap-3">
            <ThingDeviceAvatar
              deviceType={thing.deviceType}
              online={thing.online}
            />
            <ThingNameCell
              thingId={thing.thingId}
              thingName={thing.thingName ?? thing.awsThingName}
            />
          </div>
        );
      },
    },
    {
      accessorKey: "online",
      header: t("common:columns.status", "Status"),
      cell: ({ row }) => (
        <StatusCell
          online={row.original.online}
          lastSeen={row.original.lastSeen}
          t={t}
        />
      ),
    },
    {
      id: "typeModel",
      accessorKey: "deviceType",
      header: t("columns.typeModel", "Type / Model"),
      cell: ({ row }) => (
        <TypeModelCell
          type={row.original.deviceType}
          model={row.original.deviceModel}
        />
      ),
    },
    {
      accessorKey: "fwVersion",
      header: t("columns.fwVersion", "FW Version"),
      cell: ({ row }) => {
        const value = row.getValue<string | null>("fwVersion");
        if (!value) {
          return null;
        }
        return (
          <Badge
            color="info"
            variant="outline"
            className="font-normal tracking-wide rounded-full"
          >
            {value}
          </Badge>
        );
      },
    },
  ];
}
