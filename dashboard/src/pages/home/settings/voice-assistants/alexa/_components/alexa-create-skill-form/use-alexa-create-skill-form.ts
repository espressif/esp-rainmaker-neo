/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { toast } from "@espressif/dashboard-ui-components/components";
import { useCreateAlexaAutoConfiguration } from "@/api/integrations";
import { normalizeApiError } from "@/lib/normalize-api-error";
import {
  buildAlexaCreateSkillFormDefaults,
  buildAlexaCreateSkillFormSchema,
  getAlexaCreateSkillFormSchemaMessages,
  type AlexaCreateSkillFormValues,
} from "./alexa-create-skill-form.schema";
import { voidFormSubmit } from "@/lib/void-form-submit";

interface UseAlexaCreateSkillFormOptions {
  /**
   * Invoked with the created skill id once the backend accepts the request. The skill
   * exists at this point but is not configured yet, so the host uses this to start
   * polling rather than to report success.
   */
  onCreated?: (skillId: string) => void;
}

export function useAlexaCreateSkillForm({
  onCreated,
}: UseAlexaCreateSkillFormOptions) {
  const { t } = useTranslation("voice-assistants");

  const schema = useMemo(
    () => buildAlexaCreateSkillFormSchema(getAlexaCreateSkillFormSchemaMessages(t)),
    [t],
  );

  const form = useForm<AlexaCreateSkillFormValues>({
    resolver: zodResolver(schema),
    defaultValues: useMemo(() => buildAlexaCreateSkillFormDefaults(), []),
    mode: "onSubmit",
  });

  const createMutation = useCreateAlexaAutoConfiguration();

  const submit = voidFormSubmit(
    form.handleSubmit((values) => {
      // An empty field must not reach the API: there "" means "restore the default brand", which would overwrite an OEM brand nobody meant to touch.
      createMutation.mutate({ skill_name: values.skill_name, manufacturer_name: values.manufacturer_name || undefined }, {
        onSuccess: (response) => {
          toast.success(
            t(
              "alexa.autoCreate.submitSuccess",
              "Setup started. Configuration continues in the background.",
            ),
          );
          onCreated?.(response.skill_id);
        },
      });
    }),
  );

  const submitErrorMessage = useMemo(() => {
    if (!createMutation.isError) {
      return undefined;
    }
    return normalizeApiError(
      createMutation.error,
      t("alexa.autoCreate.submitError", "Failed to create the Alexa skill."),
    );
  }, [createMutation.isError, createMutation.error, t]);

  return {
    form,
    submit,
    isSubmitting: createMutation.isPending || form.formState.isSubmitting,
    submitErrorMessage,
  };
}
