/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useFormContext } from "react-hook-form";
import {
  FormControl,
  FormField,
  FormItem,
  FormMessage,
} from "@espressif/dashboard-ui-components/components";
import { QueryRuleBuilder } from "@/aws/components/query-rule-builder";
import type { CreateOtaJobFormValues } from "../../../../_schema/create-ota-job-form.schema";

/**
 * Binds the shared `QueryRuleBuilder` to the OTA form's `queryRules` field
 * array. Extracted so the target section stays under the line budget.
 */
export function TargetRulesField() {
  const { control } = useFormContext<CreateOtaJobFormValues>();

  return (
    <FormField
      control={control}
      name="queryRules"
      render={({ field }) => (
        <FormItem>
          <FormControl>
            <QueryRuleBuilder rules={field.value} onChange={field.onChange} />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  );
}
