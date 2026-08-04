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
import { Zap } from "lucide-react";
import { QueryRuleBuilder } from "@/aws/components/query-rule-builder";
import type { CreateNodeGroupFormValues } from "../../_schema/create-node-group-form.schema";
import { DYNAMIC_CARD_ID } from "../../_constants/create-node-group-form.constants";
import { applyDynamicToggle } from "../../_utils/membership-toggles";

export function DynamicGroupSection() {
  const { t } = useTranslation("node-groups");
  const { control, setValue } = useFormContext<CreateNodeGroupFormValues>();

  const createAsSubgroup = useWatch({ control, name: "createAsSubgroup" });
  const createAsDynamic = useWatch({ control, name: "createAsDynamic" });

  const cards = useMemo<SelectableCardListItem[]>(
    () => [
      {
        id: DYNAMIC_CARD_ID,
        icon: <Zap className="h-5 w-5" aria-hidden />,
        primaryText: t("new.dynamic.card.label", "Create as dynamic group"),
        secondaryText: t(
          "new.dynamic.card.description",
          "Automatically include every node that matches your rules.",
        ),
        disabled: createAsSubgroup,
      },
    ],
    [t, createAsSubgroup],
  );

  const handleToggle = useCallback(
    (next: string[]) => {
      applyDynamicToggle(next.includes(DYNAMIC_CARD_ID), setValue);
    },
    [setValue],
  );

  return (
    <div className="flex flex-col gap-6">
      <SelectableCardList
        allowMultiple
        element="switch"
        data={cards}
        value={createAsDynamic ? [DYNAMIC_CARD_ID] : []}
        onChange={handleToggle}
        aria-label={t("new.dynamic.card.label", "Create as dynamic group")}
      />

      <Alert
        type="info"
        variant="soft"
        description={t(
          "new.dynamic.info",
          "Nodes matching these rules join the group automatically. This cannot be combined with sub-group creation.",
        )}
      />

      {createAsDynamic && (
        <FormField
          control={control}
          name="queryRules"
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <QueryRuleBuilder
                  rules={field.value}
                  onChange={field.onChange}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      )}
    </div>
  );
}
