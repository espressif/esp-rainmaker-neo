/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { useFormContext } from "react-hook-form";
import {
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  Input,
} from "@espressif/dashboard-ui-components/components";
import {
  GENERATE_NODES_MAX_COUNT,
  GENERATE_NODES_MIN_COUNT,
  type GenerateNodesFormValues,
} from "../../../../_schema/generate-nodes-form.schema";

export function DeviceCountField() {
  const { t } = useTranslation("generate");
  const { control } = useFormContext<GenerateNodesFormValues>();

  return (
    <FormField
      control={control}
      name="count"
      render={({ field, fieldState }) => (
        <FormItem>
          <FormControl>
            <Input
              {...field}
              type="number"
              inputMode="numeric"
              min={GENERATE_NODES_MIN_COUNT}
              max={GENERATE_NODES_MAX_COUNT}
              error={Boolean(fieldState.error)}
              label={t("fields.count", "Number of nodes")}
              startHelperContent={t(
                "fields.countHint",
                "Maximum 20 nodes per batch.",
              )}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  );
}
