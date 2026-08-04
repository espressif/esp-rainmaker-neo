/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { useFormContext } from "react-hook-form";
import {
  FileUpload,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@espressif/dashboard-ui-components/components";
import { ThingGroupSelector } from "@/aws/components/thing-group-selector";
import type { RegisterNodesFormValues } from "../../_schema/register-nodes-form.schema";

export function BasicDetailsSection() {
  const { t } = useTranslation(["register", "common"]);
  const { control, setValue, trigger, watch } =
    useFormContext<RegisterNodesFormValues>();
  const groupName = watch("groupName");
  const subgroupName = watch("subgroupName");

  return (
    <div className="flex flex-col gap-6">
      <FormField
        control={control}
        name="certificateFiles"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t(
                "new.fields.certificateFile.label",
                "Node certificate file",
              )}
            </FormLabel>
            <FormControl>
              <FileUpload
                title={t(
                  "new.fields.certificateFile.title",
                  "Drop your certificate CSV here",
                )}
                description={t(
                  "new.fields.certificateFile.description",
                  "Only CSV files are supported.",
                )}
                browseLabel={t(
                  "common:actions.browse",
                  "Browse",
                )}
                accept=".csv"
                multiple={false}
                files={field.value ?? []}
                onFilesChange={(files) => {
                  field.onChange(files);
                  void trigger("certificateFiles");
                }}
                hideDropzoneOnFileSelect={true}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={control}
        name="groupName"
        render={() => (
          <FormItem>
            <FormControl>
              <ThingGroupSelector
                value={groupName}
                subgroupValue={subgroupName}
                topLevelOnly
                allowSubgroupSelection
                label={t(
                  "new.fields.nodeGroup.label",
                  "Choose node group",
                )}
                onSelect={(group) => {
                  setValue("groupName", group, {
                    shouldDirty: true,
                    shouldValidate: true,
                  });
                  setValue("subgroupName", undefined, {
                    shouldDirty: true,
                    shouldValidate: true,
                  });
                }}
                onSubgroupSelect={(subgroup) => {
                  setValue("subgroupName", subgroup, {
                    shouldDirty: true,
                    shouldValidate: true,
                  });
                }}
              />
            </FormControl>
            <FormDescription>
              {t(
                "new.fields.nodeGroup.description",
                "Optional. Registered nodes will be added to this group (and subgroup if selected).",
              )}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </div>
  );
}
