/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { SimplifiedDate } from "@espressif/dashboard-ui-components";
import { CopiableText } from "@espressif/dashboard-ui-components/components";
import { OtaJobAvatar } from "@/components/avatars/ota-job-avatar";
import { OtaJobStatusBadge } from "@/components/ota-job/ota-job-status-badge";
import { stripOtaPrefix } from "@/aws/services/ota.service";
import type { ThingOtaJobRow } from "../ota-jobs.props";

export function getThingOtaJobsColumns(
  t: TFunction,
): ColumnDef<ThingOtaJobRow>[] {
  return [
    {
      accessorKey: "jobId",
      header: t("common:columns.nameId", "Name / ID"),
      enableHiding: false,
      cell: ({ row }) => {
        const job = row.original;
        const displayName = stripOtaPrefix(job.jobId);
        return (
          <div className="flex min-w-0 items-center gap-3">
            <OtaJobAvatar status={job.status} />
            <div className="min-w-0 flex flex-col">
              <p className="text-sm font-semibold truncate leading-tight">
                {displayName}
              </p>
              <CopiableText
                text={job.jobId}
                className="text-xs text-muted-foreground truncate leading-tight"
              />
            </div>
          </div>
        );
      },
    },
    {
      accessorKey: "status",
      header: t("common:columns.status", "Status"),
      cell: ({ row }) => <OtaJobStatusBadge status={row.original.status} />,
    },
    {
      accessorKey: "queuedAt",
      header: t("details.otaJobs.columns.queuedAt", "Queued At"),
      cell: ({ row }) => (
        <SimplifiedDate relative ts={row.original.queuedAt?.getTime()} />
      ),
    },
    {
      accessorKey: "startedAt",
      header: t("details.otaJobs.columns.startedAt", "Started At"),
      cell: ({ row }) => (
        <SimplifiedDate relative ts={row.original.startedAt?.getTime()} />
      ),
    },
    {
      accessorKey: "lastUpdatedAt",
      header: t("common:columns.lastUpdatedAt", "Last Updated At"),
      cell: ({ row }) => (
        <SimplifiedDate relative ts={row.original.lastUpdatedAt?.getTime()} />
      ),
    },
  ];
}
