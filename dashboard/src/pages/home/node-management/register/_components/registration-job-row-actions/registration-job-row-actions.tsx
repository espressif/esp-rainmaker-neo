/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { RegistrationJobDownloadButton } from "../registration-job-download-button";
import type { RegistrationJobRowActionsProps } from "./registration-job-row-actions.props";

export function RegistrationJobRowActions({
  certFileS3Path,
  onDownload,
}: RegistrationJobRowActionsProps) {
  if (!certFileS3Path) {
    return null;
  }

  return (
    <div
      className="opacity-0 group-hover:opacity-100 focus-within:opacity-100 transition-opacity"
      onClick={(e) => e.stopPropagation()}
    >
      <RegistrationJobDownloadButton
        certFileS3Path={certFileS3Path}
        onDownload={onDownload}
      />
    </div>
  );
}
