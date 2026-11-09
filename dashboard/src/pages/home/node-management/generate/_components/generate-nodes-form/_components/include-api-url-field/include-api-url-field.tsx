/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useFormContext } from "react-hook-form";
import {
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  SelectableCardList,
  type SelectableCardListItem,
} from "@espressif/dashboard-ui-components/components";
import { Server } from "lucide-react";
import type { GenerateNodesFormValues } from "../../../../_schema/generate-nodes-form.schema";

const INCLUDE_API_URL_CARD_ID = "include-api-url";

// A controller has to reach the REST session endpoint, and the API base URL is the one bootstrap
// value it cannot derive from the IoT endpoint it already has. Writing it into NVS here saves a
// per-build menuconfig value. Off by default: ordinary nodes never call the REST API.
export function IncludeApiUrlField() {
  const { t } = useTranslation("generate");
  const { control } = useFormContext<GenerateNodesFormValues>();

  const cards = useMemo<SelectableCardListItem[]>(
    () => [
      {
        id: INCLUDE_API_URL_CARD_ID,
        icon: <Server className="h-5 w-5" />,
        primaryText: t(
          "fields.includeApiUrl",
          "Provision cloud API URL in NVS",
        ),
        secondaryText: t(
          "fields.includeApiUrlHint",
          "Writes the REST API base URL into device NVS so controller firmware can reach the session endpoint. Enable only for controller nodes.",
        ),
      },
    ],
    [t],
  );

  return (
    <FormField
      control={control}
      name="includeApiUrl"
      render={({ field }) => (
        <FormItem>
          <FormControl>
            <SelectableCardList
              allowMultiple
              data={cards}
              value={field.value ? [INCLUDE_API_URL_CARD_ID] : []}
              onChange={(next) =>
                field.onChange(next.includes(INCLUDE_API_URL_CARD_ID))
              }
              aria-label={t(
                "fields.includeApiUrlAriaLabel",
                "Controller API URL provisioning",
              )}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  );
}
