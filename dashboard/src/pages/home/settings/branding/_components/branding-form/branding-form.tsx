/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Upload, X } from "lucide-react";
import {
  Alert,
  FileUpload,
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@espressif/dashboard-ui-components/components";
import { FormFooterActions } from "@/components/form-footer-actions";
import { RAINMAKER_FILE_ACCEPT_ATTR } from "./branding-form.schema";
import { useBrandingForm } from "./use-branding-form";
import type { BrandingFormProps } from "./branding-form.props";

const ICON_CLASS = "h-4 w-4 shrink-0";

export default function BrandingForm({
  onSuccess,
  onCancel,
}: BrandingFormProps) {
  const { t } = useTranslation(["branding", "common"]);
  const { form, submit, isSubmitting, submitErrorMessage } = useBrandingForm({
    onSuccess,
  });

  return (
    <Form {...form}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-6">
        <FormField
          control={form.control}
          name="rainmakerFiles"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t("form.fileLabel", "App Studio file")}</FormLabel>
              <FormControl>
                <FileUpload
                  title={t("form.fileTitle", "Drop your .rainmaker file here")}
                  description={t(
                    "form.fileDescription",
                    "Save the project in ESP RainMaker Neo App Studio and upload the .rainmaker file it downloads.",
                  )}
                  browseLabel={t("common:actions.browse", "Browse")}
                  accept={RAINMAKER_FILE_ACCEPT_ATTR}
                  multiple={false}
                  files={field.value}
                  onFilesChange={field.onChange}
                  hideDropzoneOnFileSelect={true}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {submitErrorMessage ? (
          <Alert type="error" variant="soft" color="error">
            {submitErrorMessage}
          </Alert>
        ) : null}

        <FormFooterActions
          softAction={
            onCancel && {
              label: t("common:actions.cancel", "Cancel"),
              startIcon: <X className={ICON_CLASS} aria-hidden />,
              onClick: onCancel,
              type: "button",
            }
          }
          primaryAction={{
            label: t("form.submit", "Publish brand"),
            startIcon: <Upload className={ICON_CLASS} aria-hidden />,
            type: "submit",
            loading: isSubmitting,
          }}
        />
      </form>
    </Form>
  );
}
