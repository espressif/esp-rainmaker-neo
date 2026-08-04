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
  Textarea,
} from "@espressif/dashboard-ui-components/components";
import type { GvaConfigFormValues } from "../../gva-config-form.schema";

interface ManualFieldConfig {
  name: keyof GvaConfigFormValues;
  labelKey: string;
  placeholderKey: string;
  required?: boolean;
  multiline?: boolean;
  type?: "url";
}

/**
 * Field order mirrors the service-account JSON. Only the three core fields are
 * required (see schema); URL fields render as `type="url"` and `private_key` as
 * a textarea.
 */
const MANUAL_FIELDS: ManualFieldConfig[] = [
  { name: "type", labelKey: "gva.type", placeholderKey: "gva.typePlaceholder" },
  {
    name: "project_id",
    labelKey: "gva.projectId",
    placeholderKey: "gva.projectIdPlaceholder",
    required: true,
  },
  {
    name: "private_key_id",
    labelKey: "gva.privateKeyId",
    placeholderKey: "gva.privateKeyIdPlaceholder",
  },
  {
    name: "private_key",
    labelKey: "gva.privateKey",
    placeholderKey: "gva.privateKeyPlaceholder",
    required: true,
    multiline: true,
  },
  {
    name: "client_email",
    labelKey: "gva.clientEmail",
    placeholderKey: "gva.clientEmailPlaceholder",
    required: true,
  },
  { name: "client_id", labelKey: "gva.clientId", placeholderKey: "gva.clientIdPlaceholder" },
  {
    name: "auth_uri",
    labelKey: "gva.authUri",
    placeholderKey: "gva.authUriPlaceholder",
    type: "url",
  },
  {
    name: "token_uri",
    labelKey: "gva.tokenUri",
    placeholderKey: "gva.tokenUriPlaceholder",
    type: "url",
  },
  {
    name: "auth_provider_x509_cert_url",
    labelKey: "gva.authProviderCertUrl",
    placeholderKey: "gva.authProviderCertUrlPlaceholder",
    type: "url",
  },
  {
    name: "client_x509_cert_url",
    labelKey: "gva.clientCertUrl",
    placeholderKey: "gva.clientCertUrlPlaceholder",
    type: "url",
  },
  {
    name: "universe_domain",
    labelKey: "gva.universeDomain",
    placeholderKey: "gva.universeDomainPlaceholder",
  },
];

/**
 * All service-account credential inputs (manual-entry mode). Reads the form via
 * context so the parent view stays declarative.
 */
export default function GvaConfigFields() {
  const { t } = useTranslation("voice-assistants");
  const { control } = useFormContext<GvaConfigFormValues>();

  return (
    <div className="flex flex-col gap-5">
      {MANUAL_FIELDS.map((fieldConfig) => (
        <FormField
          key={fieldConfig.name}
          control={control}
          name={fieldConfig.name}
          render={({ field, fieldState }) => (
            <FormItem>
              <FormControl>
                {fieldConfig.multiline ? (
                  <Textarea
                    {...field}
                    label={t(fieldConfig.labelKey)}
                    required={fieldConfig.required}
                    placeholder={t(fieldConfig.placeholderKey)}
                    error={!!fieldState.error}
                    rows={4}
                  />
                ) : (
                  <Input
                    {...field}
                    type={fieldConfig.type}
                    label={t(fieldConfig.labelKey)}
                    required={fieldConfig.required}
                    placeholder={t(fieldConfig.placeholderKey)}
                    error={!!fieldState.error}
                  />
                )}
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      ))}
    </div>
  );
}
