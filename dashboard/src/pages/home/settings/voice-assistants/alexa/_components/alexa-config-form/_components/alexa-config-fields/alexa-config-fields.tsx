/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useFormContext } from "react-hook-form";
import { useTranslation } from "react-i18next";
import {
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  Input,
  InputPassword,
} from "@espressif/dashboard-ui-components/components";
import type { AlexaConfigFormValues } from "../../alexa-config-form.schema";

export default function AlexaConfigFields() {
  const { t } = useTranslation("voice-assistants");
  const { control } = useFormContext<AlexaConfigFormValues>();

  return (
    <div className="flex flex-col gap-5">
      <FormField
        control={control}
        name="client_id"
        render={({ field, fieldState }) => (
          <FormItem>
            <FormControl>
              <Input
                {...field}
                label={t("alexa.clientId", "Client ID")}
                required
                placeholder={t("alexa.clientIdPlaceholder", "Enter Alexa Skill client ID")}
                error={!!fieldState.error}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={control}
        name="client_secret"
        render={({ field, fieldState }) => (
          <FormItem>
            <FormControl>
              <InputPassword
                {...field}
                label={t("alexa.clientSecret", "Client Secret")}
                required
                placeholder={t("alexa.clientSecretPlaceholder", "Enter Alexa Skill client secret")}
                hintContent={t(
                  "alexa.form.clientSecretHint",
                  "For security the client secret is never shown. Re-enter it to save changes.",
                )}
                error={!!fieldState.error}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={control}
        name="skill_id"
        render={({ field, fieldState }) => (
          <FormItem>
            <FormControl>
              <Input
                {...field}
                label={t("alexa.skillId", "Skill ID")}
                required
                placeholder={t("alexa.skillIdPlaceholder", "Enter Alexa Skill ID")}
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
                  "alexa.form.manufacturerNameHint",
                  "Shown as the device brand in the Alexa app. Leave empty to use the default (Espressif).",
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
