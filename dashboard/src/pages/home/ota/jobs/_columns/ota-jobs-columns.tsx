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
import { cn } from "@/utils/utils";
import { getPresetColorTextClass } from "@/config/node-status.config";
import { getTargetSelectionPresentation } from "@/config/target-selection.config";
import { OtaJobsRowActions } from "../_components/ota-jobs-row-actions";
import type { OtaJobRow } from "../ota-jobs.props";

export function getOtaJobsColumns(t: TFunction): ColumnDef<OtaJobRow>[] {
  return [
    {
      accessorKey: "jobId",
      header: t("columns.name", "Name / ID"),
      enableHiding: false,
      cell: ({ row }) => {
        const job = row.original;
        const displayName = stripOtaPrefix(job.jobId);
        const targetPresentation = job.targetSelection
          ? getTargetSelectionPresentation(job.targetSelection)
          : null;
        const TargetIcon = targetPresentation?.Icon;
        const targetLabel = targetPresentation
          ? t(targetPresentation.i18nKey, targetPresentation.labelFallback)
          : undefined;
        return (
          <div className="flex min-w-0 items-center gap-3">
            <OtaJobAvatar status={job.status} />
            <div className="min-w-0 flex flex-col">
              <div className="flex min-w-0 items-center gap-2">
                <p className="text-sm font-semibold truncate leading-tight">
                  {displayName}
                </p>
                {TargetIcon ? (
                  <span title={targetLabel} className="inline-flex shrink-0">
                    <TargetIcon
                      className={cn(
                        "h-3.5 w-3.5",
                        getPresetColorTextClass(targetPresentation.color),
                      )}
                      aria-label={targetLabel}
                    />
                  </span>
                ) : null}
              </div>
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
      accessorKey: "createdAt",
      header: t("common:columns.createdAt", "Created At"),
      cell: ({ row }) => (
        <SimplifiedDate relative ts={row.original.createdAt?.getTime()} />
      ),
    },
    {
      id: "actions",
      header: "",
      enableSorting: false,
      enableHiding: false,
      size: 160,
      cell: ({ row }) => (
        <div className="flex justify-end">
          <OtaJobsRowActions
            jobId={row.original.jobId}
            status={row.original.status}
          />
        </div>
      ),
    },
  ];
}
