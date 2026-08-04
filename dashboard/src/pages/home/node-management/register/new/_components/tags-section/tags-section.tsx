/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { useFormContext } from "react-hook-form";
import {
  Alert,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  TagInput,
} from "@espressif/dashboard-ui-components/components";
import type { RegisterNodesFormValues } from "../../_schema/register-nodes-form.schema";

const TAG_REGEX = /^[^:\s]+:[^:\s]+$/;

export function TagsSection() {
  const { t } = useTranslation("register");
  const { control } = useFormContext<RegisterNodesFormValues>();

  return (
    <div className="flex flex-col gap-4">
      <Alert
        variant="soft"
        color="info"
        type="info"
        title={t(
          "new.tags.autoTagTitle",
          "Some tags will be added automatically",
        )}
        description={t(
          "new.tags.autoTagNotice",
          "The following tags will be added automatically: registered_from:dashboard, batch:<timestamp>. You can override them by adding tags with the same key above.",
        )}
      />

      <FormField
        control={control}
        name="tags"
        render={({ field }) => (
          <FormItem>
            <FormControl>
              <TagInput
                label={t("new.tags.label", "Tags")}
                placeholder={t(
                  "new.tags.placeholder",
                  "key:value and press Enter",
                )}
                tags={field.value ?? []}
                onTagsChange={(tags) => field.onChange(tags)}
                validatorRegex={TAG_REGEX}
                nameValueTags
                hintContent={t(
                  "new.tags.hint",
                  "Format: key:value (no spaces).",
                )}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </div>
  );
}
