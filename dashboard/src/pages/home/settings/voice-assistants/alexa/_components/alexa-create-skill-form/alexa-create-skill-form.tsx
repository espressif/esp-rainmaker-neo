/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useFormContext } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Sparkles, X } from "lucide-react";
import {
  Alert,
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  Input,
  Typography,
} from "@espressif/dashboard-ui-components/components";
import { useGetAlexaConfig } from "@/api/integrations";
import { FormFooterActions } from "@/components/form-footer-actions";
import { AlexaLwaConnection } from "../alexa-lwa-connection";
import { useAlexaCreateSkillForm } from "./use-alexa-create-skill-form";
import type { AlexaCreateSkillFormValues } from "./alexa-create-skill-form.schema";
import { SUGGESTED_ALEXA_SKILL_NAME } from "../../alexa.constants";
import type { AlexaCreateSkillFormProps } from "./alexa-create-skill-form.props";

const ICON_CLASS = "h-4 w-4 shrink-0";

function CreateSkillFields() {
  const { t } = useTranslation("voice-assistants");
  const { control } = useFormContext<AlexaCreateSkillFormValues>();

  return (
    <div className="flex flex-col gap-5">
      <FormField
        control={control}
        name="skill_name"
        render={({ field, fieldState }) => (
          <FormItem>
            <FormControl>
              <Input
                {...field}
                label={t("alexa.autoCreate.skillName", "Skill Name")}
                required
                placeholder={t("alexa.autoCreate.skillNamePlaceholder", "e.g. {{name}}", {
                  name: SUGGESTED_ALEXA_SKILL_NAME,
                })}
                error={!!fieldState.error}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={control}
        name="manufacturer_name"
        render={({ field, fieldState }) => (
          <FormItem>
            <FormControl>
              <Input
                {...field}
                label={t("alexa.manufacturerName", "Manufacturer Name")}
                placeholder={t(
                  "alexa.manufacturerNamePlaceholder",
                  "Enter the brand shown in Alexa discovery",
                )}
                hintContent={t(
                  "alexa.autoCreate.manufacturerNameHint",
                  "Leave empty to keep the current brand.",
                )}
                error={!!fieldState.error}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </div>
  );
}

/**
 * Automatic mode: the backend builds and configures the skill over SMAPI, signed in as the
 * connected Amazon developer account, instead of the operator copying skill credentials out
 * of the Alexa console. Creation is only the first step, the caller polls config_status
 * once {@link AlexaCreateSkillFormProps.onCreated} fires.
 */
export default function AlexaCreateSkillForm({
  onCancel,
  onCreated,
}: AlexaCreateSkillFormProps) {
  const { t } = useTranslation(["voice-assistants", "common"]);
  const { form, submit, isSubmitting, submitErrorMessage } =
    useAlexaCreateSkillForm({ onCreated });
  const connectedClientId = useGetAlexaConfig().data?.lwa_client_id;

  return (
    <Form {...form}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-6">
        <AlexaLwaConnection connectedClientId={connectedClientId} />

        <CreateSkillFields />

        {connectedClientId ? null : (
          <Typography variant="body2" as="p" className="text-muted-foreground">
            {t(
              "alexa.lwa.connectFirst",
              "Connect an Amazon account above first.",
            )}
          </Typography>
        )}

        {submitErrorMessage ? (
          <Alert type="error" variant="soft" color="error">
            {submitErrorMessage}
          </Alert>
        ) : null}

        <FormFooterActions
          softAction={{
            label: t("common:actions.cancel", "Cancel"),
            startIcon: <X className={ICON_CLASS} aria-hidden />,
            onClick: onCancel,
            type: "button",
            disabled: isSubmitting,
          }}
          primaryAction={{
            label: t("alexa.autoCreate.submitButton", "Set up skill"),
            startIcon: <Sparkles className={ICON_CLASS} aria-hidden />,
            type: "submit",
            loading: isSubmitting,
            disabled: !connectedClientId,
          }}
        />
      </form>
    </Form>
  );
}
