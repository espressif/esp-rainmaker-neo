/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";
import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { format } from "date-fns";
import { Tooltip } from "@espressif/dashboard-ui-components";
import { ThingDeviceAvatar } from "@/components/avatars/thing-device-avatar";
import { ThingNameCell } from "@/aws/components/thing-name-cell/thing-name-cell";
import { ThingStatusBadge } from "@/components/thing/thing-status-badge";
import type { AwsThingRow } from "../use-aws-things-list";

type RenderRowActions = (row: AwsThingRow) => ReactNode;

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

export function getAwsThingsColumns(
  t: TFunction,
  renderRowActions?: RenderRowActions,
): ColumnDef<AwsThingRow>[] {
  return [
    {
      accessorKey: "thingName",
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
              thingId={thing.thingId ?? thing.thingName}
              thingName={thing.displayName ?? thing.thingName}
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
      id: "actions",
      header: "",
      cell: ({ row }) =>
        renderRowActions ? (
          <div className="flex items-center justify-end opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
            {renderRowActions(row.original)}
          </div>
        ) : null,
    },
  ];
}
