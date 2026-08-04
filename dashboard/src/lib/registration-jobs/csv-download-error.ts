/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { CsvNotAvailableError } from "./download-cert-csv";

/**
 * Minimal structural shape of i18next's `t`, so this helper works with a `t` bound
 * to one namespace or to several without dragging in i18next's generics.
 */
type TranslateFn = (key: string, defaultValue: string) => string;

/** Translate a {@link downloadCertCsv} rejection for display. */
export function getCsvDownloadErrorMessage(
  error: unknown,
  t: TranslateFn,
): string {
  if (error instanceof CsvNotAvailableError) {
    return t(
      "fileUnavailable",
      "This file is no longer available. It may have been removed from storage.",
    );
  }
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return t("downloadFailed", "Download failed");
}
