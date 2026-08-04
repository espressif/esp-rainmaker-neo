/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { useFormContext } from "react-hook-form";
import {
  FileUpload,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@espressif/dashboard-ui-components/components";
import { stripExtension } from "@/aws/components/firmware-upload/firmware-upload.utils";
import {
  OTA_IMAGE_ACCEPT_ATTR,
  type UploadOtaImageFormValues,
} from "../../_schema/upload-ota-image-form.schema";

export function SelectOtaImageSection() {
  const { t } = useTranslation(["ota-images", "common"]);
  const { control, setValue, getValues, trigger } =
    useFormContext<UploadOtaImageFormValues>();

  return (
    <FormField
      control={control}
      name="firmwareFiles"
      render={({ field }) => (
        <FormItem>
          <FormLabel>
            {t("fields.firmwareFile.label", "Firmware file")}
          </FormLabel>
          <FormControl>
            <FileUpload
              title={t(
                "fields.firmwareFile.title",
                "Drop your firmware file here",
              )}
              description={t(
                "fields.firmwareFile.description",
                "Supported file types: .bin, .elf, .img, .hex, .ota.",
              )}
              browseLabel={t(
                "common:actions.browse",
                "Browse",
              )}
              accept={OTA_IMAGE_ACCEPT_ATTR}
              multiple={false}
              files={field.value ?? []}
              onFilesChange={(files) => {
                field.onChange(files);
                const selected = files[0];
                if (selected && !getValues("name")) {
                  setValue("name", stripExtension(selected.name), {
                    shouldDirty: true,
                    shouldValidate: true,
                  });
                }
                void trigger("firmwareFiles");
              }}
              hideDropzoneOnFileSelect={true}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  );
}
