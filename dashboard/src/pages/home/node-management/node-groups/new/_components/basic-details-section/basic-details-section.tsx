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
  Textarea,
} from "@espressif/dashboard-ui-components/components";
import type { CreateNodeGroupFormValues } from "../../_schema/create-node-group-form.schema";

export function BasicDetailsSection() {
  const { t } = useTranslation("node-groups");
  const { control } = useFormContext<CreateNodeGroupFormValues>();

  return (
    <div className="flex flex-col gap-6">
      <FormField
        control={control}
        name="groupName"
        render={({ field, fieldState }) => (
          <FormItem>
            <FormControl>
              <Input
                {...field}
                label={t("new.fields.name.label", "Group name")}
                placeholder={t(
                  "new.fields.name.placeholder",
                  "Enter a name for this group",
                )}
                required
                autoComplete="off"
                error={!!fieldState.error}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={control}
        name="description"
        render={({ field, fieldState }) => (
          <FormItem>
            <FormControl>
              <Textarea
                {...field}
                label={t("new.fields.description.label", "Description")}
                placeholder={t(
                  "new.fields.description.placeholder",
                  "Optional description",
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
