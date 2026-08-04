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

function clampCount(raw: string): number {
  const parsed = Number.parseInt(raw, 10);
  if (Number.isNaN(parsed)) {
    return GENERATE_NODES_MIN_COUNT;
  }
  return Math.min(
    GENERATE_NODES_MAX_COUNT,
    Math.max(GENERATE_NODES_MIN_COUNT, parsed),
  );
}

export function DeviceCountField() {
  const { t } = useTranslation("generate");
  const { control } = useFormContext<GenerateNodesFormValues>();

  return (
    <FormField
      control={control}
      name="count"
      render={({ field }) => (
        <FormItem>
          <FormControl>
            <Input
              type="number"
              min={GENERATE_NODES_MIN_COUNT}
              max={GENERATE_NODES_MAX_COUNT}
              name={field.name}
              ref={field.ref}
              value={field.value}
              onBlur={field.onBlur}
              onChange={(event) => field.onChange(clampCount(event.target.value))}
              label={t("fields.count", "Number of devices")}
              startHelperContent={t(
                "fields.countHint",
                "Maximum 20 devices per batch.",
              )}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  );
}
