/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { CopiableText } from "@espressif/dashboard-ui-components/components";
import { RegistrationJobAvatar } from "@/components/avatars/registration-job-avatar";
import { getRegistrationJobFileName } from "@/lib/registration-jobs/registration-job-display";
import type { RegistrationJobFileNameCellProps } from "./registration-job-file-name-cell.props";

export function RegistrationJobFileNameCell({
  requestId,
  certFileS3Path,
  failedCount,
}: RegistrationJobFileNameCellProps) {
  const { t } = useTranslation("register");
  const displayName =
    getRegistrationJobFileName(certFileS3Path) ??
    t("unknownFile", "Registration job");

  return (
    <div className="flex min-w-0 items-center gap-3">
      <RegistrationJobAvatar failedCount={failedCount} />
      <div className="min-w-0 flex flex-col">
        <p className="text-sm font-semibold truncate leading-tight">
          {displayName}
        </p>
        <CopiableText
          text={requestId}
          className="text-xs text-muted-foreground truncate leading-tight"
        />
      </div>
    </div>
  );
}
