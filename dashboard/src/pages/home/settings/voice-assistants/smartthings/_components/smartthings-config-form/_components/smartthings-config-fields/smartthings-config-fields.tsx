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
} from "@espressif/dashboard-ui-components/components";
import type { SmartThingsConfigFormValues } from "../../smartthings-config-form.schema";

export default function SmartThingsConfigFields() {
  const { control } = useFormContext<SmartThingsConfigFormValues>();
  const { t } = useTranslation("voice-assistants");

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
                label={t("smartthings.clientId", "Client ID")}
                required
                placeholder={t(
                  "smartthings.clientIdPlaceholder",
                  "Client ID from the SmartThings Developer Workspace",
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
        name="client_secret"
        render={({ field, fieldState }) => (
          <FormItem>
            <FormControl>
              <Input
                {...field}
                type="password"
                autoComplete="new-password"
                label={t("smartthings.clientSecret", "Client Secret")}
                required
                placeholder={t(
                  "smartthings.clientSecretPlaceholder",
                  "Client Secret issued with the Client ID",
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
