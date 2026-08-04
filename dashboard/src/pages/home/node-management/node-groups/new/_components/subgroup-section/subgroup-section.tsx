/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useFormContext, useWatch } from "react-hook-form";
import {
  Alert,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  SelectableCardList,
  type SelectableCardListItem,
} from "@espressif/dashboard-ui-components/components";
import { FolderTree } from "lucide-react";
import { ThingGroupSelector } from "@/aws/components/thing-group-selector/thing-group-selector";
import type { CreateNodeGroupFormValues } from "../../_schema/create-node-group-form.schema";
import { SUBGROUP_CARD_ID } from "../../_constants/create-node-group-form.constants";
import { applySubgroupToggle } from "../../_utils/membership-toggles";

export function SubgroupSection() {
  const { t } = useTranslation("node-groups");
  const { control, setValue, setError } =
    useFormContext<CreateNodeGroupFormValues>();

  const createAsSubgroup = useWatch({ control, name: "createAsSubgroup" });
  const createAsDynamic = useWatch({ control, name: "createAsDynamic" });

  const cards = useMemo<SelectableCardListItem[]>(
    () => [
      {
        id: SUBGROUP_CARD_ID,
        icon: <FolderTree className="h-5 w-5" aria-hidden />,
        primaryText: t("new.subgroup.card.label", "Create as sub-group"),
        secondaryText: t(
          "new.subgroup.card.description",
          "Nest this group under an existing parent group.",
        ),
        disabled: createAsDynamic,
      },
    ],
    [t, createAsDynamic],
  );

  const handleToggle = useCallback(
    (next: string[]) => {
      applySubgroupToggle(next.includes(SUBGROUP_CARD_ID), setValue);
    },
    [setValue],
  );

  const handleParentError = useCallback(
    (error: Error) => {
      setError("parentGroupName", { type: "manual", message: error.message });
    },
    [setError],
  );

  return (
    <div className="flex flex-col gap-6">
      <SelectableCardList
        allowMultiple
        element="switch"
        data={cards}
        value={createAsSubgroup ? [SUBGROUP_CARD_ID] : []}
        onChange={handleToggle}
        aria-label={t("new.subgroup.card.label", "Create as sub-group")}
      />

      <Alert
        type="info"
        variant="soft"
        description={t(
          "new.subgroup.info",
          "Pick a parent group below to nest this group. A sub-group cannot also be a dynamic group.",
        )}
      />

      {createAsSubgroup && (
        <div className="rounded-lg border border-border bg-muted/20 p-4">
          <FormField
            control={control}
            name="parentGroupName"
            render={({ field }) => (
              <FormItem>
                <FormControl>
                  <ThingGroupSelector
                    value={field.value || undefined}
                    onSelect={(next) => field.onChange(next ?? "")}
                    onError={handleParentError}
                    label={t("new.subgroup.parent.label", "Parent group")}
                    placeholder={t(
                      "new.subgroup.parent.placeholder",
                      "Select a parent group",
                    )}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>
      )}
    </div>
  );
}
