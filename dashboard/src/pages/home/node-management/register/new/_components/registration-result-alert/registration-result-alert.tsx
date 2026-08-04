/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Alert } from "@espressif/dashboard-ui-components/components";
import { useDownloadRegistrationCsv } from "@/api/node-registration";
import { getCsvDownloadErrorMessage } from "@/lib/registration-jobs/csv-download-error";
import { RegistrationJobDownloadButton } from "../../../_components/registration-job-download-button";
import type { RegistrationResultAlertProps } from "./registration-result-alert.props";

/**
 * Terminal outcome of a bulk registration, rendered under the progress steps.
 * Owns its own download state, so unmounting on retry/close resets it.
 */
export function RegistrationResultAlert({
  result,
}: RegistrationResultAlertProps) {
  const { t } = useTranslation("register");
  const downloadMutation = useDownloadRegistrationCsv();
  const { mutate: downloadCsv } = downloadMutation;

  const handleDownload = useCallback(
    (s3Path: string) => {
      downloadCsv(s3Path);
    },
    [downloadCsv],
  );

  return (
    <div className="flex w-full flex-col gap-2">
      <Alert
        variant="soft"
        type={result.type}
        title={result.title}
        description={result.description}
        action={
          <RegistrationJobDownloadButton
            certFileS3Path={result.failedFileS3Path}
            onDownload={handleDownload}
            disabled={downloadMutation.isPending}
            label={t(
              "new.progress.resultAlert.downloadFailedNodes",
              "Download failed nodes",
            )}
          />
        }
      />

      {downloadMutation.error && (
        <Alert
          variant="outline"
          type="error"
          title={t(
            "new.progress.resultAlert.downloadErrorTitle",
            "Could not download the failed nodes file",
          )}
          description={getCsvDownloadErrorMessage(downloadMutation.error, t)}
        />
      )}
    </div>
  );
}
