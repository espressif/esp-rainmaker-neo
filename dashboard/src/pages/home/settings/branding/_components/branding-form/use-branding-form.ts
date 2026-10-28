/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { toast } from "@espressif/dashboard-ui-components/components";
import { usePublishBrandMutation } from "@/api/branding";
import { voidFormSubmit } from "@/lib/void-form-submit";
import {
  BRANDING_FORM_DEFAULTS,
  buildBrandingFormSchema,
  getBrandingFormSchemaMessages,
  type BrandingFormValues,
} from "./branding-form.schema";
import type { BrandingFormProps } from "./branding-form.props";
import { getPublishErrorMessage } from "./branding-form.utils";

export function useBrandingForm({
  onSuccess,
}: Pick<BrandingFormProps, "onSuccess">) {
  const { t } = useTranslation("branding");

  const schema = useMemo(
    () => buildBrandingFormSchema(getBrandingFormSchemaMessages(t)),
    [t],
  );

  const form = useForm<BrandingFormValues>({
    resolver: zodResolver(schema),
    defaultValues: BRANDING_FORM_DEFAULTS,
    mode: "onSubmit",
  });

  const publishMutation = usePublishBrandMutation();

  const submit = voidFormSubmit(
    form.handleSubmit((values) => {
      publishMutation.mutate(values.rainmakerFiles[0], {
        onSuccess: (brand) => {
          toast.success(
            t(
              "form.published",
              "{{brandName}} is published. Phones download it the next time ESP RainMaker Home opens and show it from the launch after.",
              { brandName: brand.config.brandName },
            ),
          );
          onSuccess?.();
        },
      });
    }),
  );

  const submitErrorMessage = useMemo(() => {
    if (!publishMutation.error) {
      return undefined;
    }
    return getPublishErrorMessage(publishMutation.error, t);
  }, [publishMutation.error, t]);

  return {
    form,
    submit,
    isSubmitting: publishMutation.isPending,
    submitErrorMessage,
  };
}
