/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useFormContext } from "react-hook-form";
import { useTranslation } from "react-i18next";
import {
  FileUpload,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@espressif/dashboard-ui-components/components";
import IntegrationHelpCard from "../integration-help-card";
import type { PushIntegrationFormValues } from "../../push-integration-form.schema";

const ACCEPTED_FILE_TYPES = ".json,application/json";

/** FCM service-account upload, shown when the Android platform is selected. */
export default function FcmFields() {
  const { t } = useTranslation(["push-notifications", "common"]);
  const { control } = useFormContext<PushIntegrationFormValues>();

  return (
    <div className="flex flex-col gap-5">
      <IntegrationHelpCard type="android" />

      <FormField
        control={control}
        name="service_account"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("form.serviceAccountLabel", "Service account JSON")}
            </FormLabel>
            <FormControl>
              <FileUpload
                accept={ACCEPTED_FILE_TYPES}
                title={t(
                  "form.serviceAccountUploadTitle",
                  "Drop your service account JSON here",
                )}
                description={t(
                  "form.serviceAccountUploadDescription",
                  "Only .json files are accepted.",
                )}
                browseLabel={t("common:actions.browse", "Browse")}
                files={field.value}
                onFilesChange={field.onChange}
                hideDropzoneOnFileSelect={true}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </div>
  );
}
