/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { CheckCircle2 } from "lucide-react";
import {
  Alert,
  FileUpload,
  Typography,
} from "@espressif/dashboard-ui-components/components";
import type { GvaJsonUploadFieldProps } from "./gva-json-upload-field.props";

/**
 * Service-account JSON picker (upload mode). `FileUpload` returns `File[]`, not
 * file content, so the parent hook reads and parses the JSON. Only `.json` files
 * are offered in the OS picker; validation happens on parse.
 */
export default function GvaJsonUploadField({
  fileName,
  fileError,
  onFilesChange,
}: GvaJsonUploadFieldProps) {
  const { t } = useTranslation("voice-assistants");

  return (
    <div className="flex flex-col gap-3">
      <FileUpload
        accept=".json,application/json"
        description={t(
          "gva.form.uploadDescription",
          "Upload your Google service account JSON file to auto-fill the credentials.",
        )}
        onFilesChange={onFilesChange}
        hideDropzoneOnFileSelect={true}
      />

      {fileName && !fileError ? (
        <Typography
          variant="body2"
          as="p"
          className="flex items-center gap-2 text-muted-foreground"
        >
          <CheckCircle2 className="h-4 w-4 shrink-0 text-success" aria-hidden />
          {t("gva.form.uploadedFile", "Selected file: {{name}}", { name: fileName })}
        </Typography>
      ) : null}

      {fileError ? (
        <Alert type="error" variant="soft" color="error">
          {fileError}
        </Alert>
      ) : null}
    </div>
  );
}
