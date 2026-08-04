/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Controller, useFormContext } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { FlaskConical } from "lucide-react";
import {
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  Input,
  SelectableCardList,
  Textarea,
  type SelectableCardListItem,
} from "@espressif/dashboard-ui-components/components";
import IntegrationHelpCard from "../integration-help-card";
import type { PushIntegrationFormValues } from "../../push-integration-form.schema";

const SANDBOX_ID = "sandbox";

/** iOS text fields — label + placeholder keys reuse the existing `push` copy. */
const TEXT_FIELDS = [
  { name: "bundle_id", labelKey: "bundleId", placeholderKey: "bundleIdPlaceholder" },
  { name: "key_id", labelKey: "keyId", placeholderKey: "keyIdPlaceholder" },
  { name: "team_id", labelKey: "teamId", placeholderKey: "teamIdPlaceholder" },
] as const;

/** APNS credential fields, shown when the iOS platform is selected. */
export default function ApnsFields() {
  const { t } = useTranslation("push-notifications");
  const { control } = useFormContext<PushIntegrationFormValues>();

  const sandboxItems: SelectableCardListItem[] = [
    {
      id: SANDBOX_ID,
      icon: <FlaskConical className="h-5 w-5" aria-hidden />,
      primaryText: t("form.sandboxPrimary", "Use APNS sandbox"),
      secondaryText: t(
        "form.sandboxSecondary",
        "Enable only for development builds.",
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-5">
      <IntegrationHelpCard type="ios" />

      {TEXT_FIELDS.map(({ name, labelKey, placeholderKey }) => (
        <FormField
          key={name}
          control={control}
          name={name}
          render={({ field, fieldState }) => (
            <FormItem>
              <FormControl>
                <Input
                  {...field}
                  label={t(labelKey)}
                  required
                  placeholder={t(placeholderKey)}
                  error={!!fieldState.error}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      ))}

      <FormField
        control={control}
        name="authentication_key"
        render={({ field, fieldState }) => (
          <FormItem>
            <FormControl>
              <Textarea
                {...field}
                label={t("authKey", "Authentication Key (.p8)")}
                required
                placeholder={t("authKeyPlaceholder", "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----")}
                error={!!fieldState.error}
                rows={4}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <Controller
        control={control}
        name="sandbox"
        render={({ field }) => (
          <SelectableCardList
            aria-label={t("form.sandboxPrimary", "Use APNS sandbox")}
            data={sandboxItems}
            allowMultiple
            element="switch"
            size="sm"
            value={field.value ? [SANDBOX_ID] : []}
            onChange={(selected) => field.onChange(selected.includes(SANDBOX_ID))}
          />
        )}
      />
    </div>
  );
}
