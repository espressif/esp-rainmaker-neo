/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Download } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@espressif/dashboard-ui-components/components";
import type { RegistrationJobDownloadButtonProps } from "./registration-job-download-button.props";

export function RegistrationJobDownloadButton({
  certFileS3Path,
  onDownload,
  label,
  disabled = false,
}: RegistrationJobDownloadButtonProps) {
  const { t } = useTranslation(["register", "common"]);

  if (!certFileS3Path) {
    return null;
  }

  return (
    <Button
      type="button"
      variant="outline"
      color="primary"
      size="sm"
      fullWidth={false}
      disabled={disabled}
      startIcon={<Download className="h-4 w-4" aria-hidden />}
      onClick={(e) => {
        e.stopPropagation();
        onDownload(certFileS3Path);
      }}
    >
      {label ?? t("common:actions.download", "Download")}
    </Button>
  );
}
