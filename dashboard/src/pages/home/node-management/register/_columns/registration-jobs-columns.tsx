/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { SimplifiedDate } from "@espressif/dashboard-ui-components";
import { RegistrationJobFileNameCell } from "../_components/registration-job-file-name-cell";
import { RegistrationJobStatusBadge } from "../_components/registration-job-status-badge";
import { RegistrationJobProgressCell } from "../_components/registration-job-progress-cell";
import { RegistrationJobRowActions } from "../_components/registration-job-row-actions";
import type { RegistrationJobRow } from "../register.props";

export function getRegistrationJobsColumns(
  t: TFunction,
  onDownload: (s3Path: string) => void,
): ColumnDef<RegistrationJobRow>[] {
  return [
    {
      id: "name",
      accessorKey: "requestId",
      header: t("columns.fileRequestId", "File / Request ID"),
      enableHiding: false,
      cell: ({ row }) => (
        <RegistrationJobFileNameCell
          requestId={row.original.requestId}
          certFileS3Path={row.original.certFileS3Path}
          failedCount={row.original.failedCount}
        />
      ),
    },
    {
      id: "status",
      accessorKey: "status",
      header: t("common:columns.status", "Status"),
      cell: ({ row }) => (
        <RegistrationJobStatusBadge status={row.original.status} />
      ),
    },
    {
      id: "progress",
      accessorKey: "totalNodes",
      header: t("columns.progress", "Progress"),
      cell: ({ row }) => (
        <RegistrationJobProgressCell
          successCount={row.original.successCount}
          failedCount={row.original.failedCount}
          totalNodes={row.original.totalNodes}
        />
      ),
    },
    {
      id: "lastUpdated",
      accessorKey: "lastUpdatedAt",
      header: t("columns.lastUpdated", "Last updated"),
      cell: ({ row }) => {
        const ts = row.original.lastUpdatedAt;
        if (!ts) {
          return null;
        }
        return <SimplifiedDate relative ts={ts * 1000} />;
      },
    },
    {
      id: "actions",
      header: "",
      enableSorting: false,
      enableHiding: false,
      size: 140,
      cell: ({ row }) => (
        <div className="flex justify-end">
          <RegistrationJobRowActions
            certFileS3Path={row.original.certFileS3Path}
            onDownload={onDownload}
          />
        </div>
      ),
    },
  ];
}
