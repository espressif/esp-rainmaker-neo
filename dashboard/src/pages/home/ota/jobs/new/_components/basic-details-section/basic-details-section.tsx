/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { useFormContext } from "react-hook-form";
import {
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  Input,
} from "@espressif/dashboard-ui-components/components";
import { S3ListObjectsSelector } from "@/aws/components/s3-list-objects-selector/s3-list-objects-selector";
import type { S3Object } from "@/aws/services/s3.service";
import { getOtaS3Bucket } from "@/lib/config";
import type { CreateOtaJobFormValues } from "../../_schema/create-ota-job-form.schema";
import { FirmwareOptionRow } from "./firmware-option-row";

export function BasicDetailsSection() {
  const { t } = useTranslation(["ota-jobs", "common"]);
  const { control, setError, setValue } =
    useFormContext<CreateOtaJobFormValues>();

  const handleFirmwareError = useCallback(
    (error: Error) => {
      setError("firmwareKey", { type: "manual", message: error.message });
    },
    [setError],
  );

  // Persist both the key and the selected object's ETag (used as the OTA job's
  // file_md5) synchronously on selection, so no effect is needed to derive it.
  const handleFirmwareSelect = useCallback(
    (next: string | string[] | undefined, selected?: S3Object[]) => {
      const key = typeof next === "string" ? next : "";
      setValue("firmwareKey", key, { shouldValidate: true, shouldDirty: true });
      setValue("fileMd5", key ? (selected?.[0]?.etag ?? "") : "");
    },
    [setValue],
  );

  return (
    <div className="flex flex-col gap-6">
      <FormField
        control={control}
        name="name"
        render={({ field, fieldState }) => (
          <FormItem>
            <FormControl>
              <Input
                {...field}
                label={t("common:columns.name", "Name")}
                placeholder={t(
                  "createOtaJobPage.name.placeholder",
                  "Enter a name for this OTA job",
                )}
                required
                autoComplete="off"
                error={!!fieldState.error}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={control}
        name="firmwareKey"
        render={({ field }) => (
          <FormItem>
            <FormControl>
              <S3ListObjectsSelector
                bucket={getOtaS3Bucket()}
                value={field.value || undefined}
                onSelect={handleFirmwareSelect}
                onError={handleFirmwareError}
                resolveValueOnLoad
                label={t("createOtaJobPage.firmware.label", "Firmware Image")}
                placeholder={t(
                  "createOtaJobPage.firmware.placeholder",
                  "Select a firmware image",
                )}
                formatOption={(object) => ({
                  value: object.key,
                  label: object.key.split("/").pop() ?? object.key,
                })}
                renderOption={(option, object) => (
                  <FirmwareOptionRow
                    name={option.label}
                    size={object?.size}
                    lastModified={object?.lastModified}
                  />
                )}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </div>
  );
}
