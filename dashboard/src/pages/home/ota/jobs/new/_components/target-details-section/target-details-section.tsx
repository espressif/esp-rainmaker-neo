/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useFormContext, useWatch } from "react-hook-form";
import {
  FormControl,
  FormField,
  FormItem,
  FormMessage,
  SelectableCardList,
  type SelectableCardListItem,
} from "@espressif/dashboard-ui-components/components";
import { Boxes, ListFilter, Network, RefreshCw } from "lucide-react";
import { ThingGroupSelector } from "@/aws/components/thing-group-selector/thing-group-selector";
import { ThingSelector } from "@/aws/components/thing-selector/thing-selector";
import { TargetRulesField } from "./_components/target-rules-field";
import type { CreateOtaJobFormValues } from "../../_schema/create-ota-job-form.schema";
import {
  CONTINUOUS_CARD_ID,
  JOB_MODE_CONTINUOUS,
  JOB_MODE_SNAPSHOT,
  SOURCE_EXISTING,
  SOURCE_RULES,
  TARGET_TYPE_GROUP,
  TARGET_TYPE_NODE,
} from "../../_constants/create-ota-job-form.constants";

type TargetType = CreateOtaJobFormValues["targetType"];

export function TargetDetailsSection() {
  const { t } = useTranslation("ota-jobs");
  const { control, setValue, setError } =
    useFormContext<CreateOtaJobFormValues>();

  const targetType = useWatch({ control, name: "targetType" });
  const targetSelection = useWatch({ control, name: "targetSelection" });
  const source = useWatch({ control, name: "source" });

  const isGroup = targetType === TARGET_TYPE_GROUP;
  const isContinuous = targetSelection === JOB_MODE_CONTINUOUS;
  const showSource = isGroup && isContinuous;
  const showRules = showSource && source === SOURCE_RULES;

  const typeCards = useMemo<SelectableCardListItem[]>(
    () => [
      {
        id: TARGET_TYPE_GROUP,
        icon: <Boxes className="h-5 w-5" aria-hidden />,
        primaryText: t("createOtaJobPage.type.group.label", "Node group"),
        secondaryText: t(
          "createOtaJobPage.type.group.description",
          "Roll out to the members of a node group.",
        ),
      },
      {
        id: TARGET_TYPE_NODE,
        icon: <Network className="h-5 w-5" aria-hidden />,
        primaryText: t("createOtaJobPage.type.node.label", "Node"),
        secondaryText: t(
          "createOtaJobPage.type.node.description",
          "Roll out to a single node.",
        ),
      },
    ],
    [t],
  );

  const continuousCards = useMemo<SelectableCardListItem[]>(
    () => [
      {
        id: CONTINUOUS_CARD_ID,
        icon: <RefreshCw className="h-5 w-5" aria-hidden />,
        primaryText: t("createOtaJobPage.continuous.label", "Continuous"),
        secondaryText: t(
          "createOtaJobPage.continuous.description",
          "New nodes that match the target will automatically receive this update.",
        ),
      },
    ],
    [t],
  );

  const sourceCards = useMemo<SelectableCardListItem[]>(
    () => [
      {
        id: SOURCE_EXISTING,
        icon: <Boxes className="h-5 w-5" aria-hidden />,
        primaryText: t("createOtaJobPage.source.existing.label", "Existing groups"),
        secondaryText: t(
          "createOtaJobPage.source.existing.description",
          "Target the current members of the selected node group.",
        ),
      },
      {
        id: SOURCE_RULES,
        icon: <ListFilter className="h-5 w-5" aria-hidden />,
        primaryText: t("createOtaJobPage.source.rules.label", "Define rules"),
        secondaryText: t(
          "createOtaJobPage.source.rules.description",
          "Target nodes that match a set of rules.",
        ),
      },
    ],
    [t],
  );

  const handleTypeChange = useCallback(
    (next: string) => {
      setValue("targetType", next as TargetType);
      setValue("targetName", "");
      setValue("targetSelection", JOB_MODE_SNAPSHOT);
      setValue("source", "");
      setValue("queryRules", []);
    },
    [setValue],
  );

  const handleContinuousChange = useCallback(
    (next: string[]) => {
      const enabled = next.includes(CONTINUOUS_CARD_ID);
      setValue(
        "targetSelection",
        enabled ? JOB_MODE_CONTINUOUS : JOB_MODE_SNAPSHOT,
      );
      if (!enabled) {
        setValue("source", "");
        setValue("queryRules", []);
      }
    },
    [setValue],
  );

  const handleSourceChange = useCallback(
    (next: string) => {
      setValue("source", next);
      if (next !== SOURCE_RULES) {
        setValue("queryRules", []);
      }
    },
    [setValue],
  );

  const handleTargetError = useCallback(
    (error: Error) => {
      setError("targetName", { type: "manual", message: error.message });
    },
    [setError],
  );

  return (
    <div className="flex flex-col gap-6">
      <SelectableCardList
        data={typeCards}
        value={targetType}
        onChange={handleTypeChange}
        aria-label={t("createOtaJobPage.type.label", "Type")}
      />

      {isGroup && (
        <FormField
          control={control}
          name="targetName"
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <ThingGroupSelector
                  value={field.value || undefined}
                  onSelect={(next) => field.onChange(next ?? "")}
                  onError={handleTargetError}
                  label={t("createOtaJobPage.nodeGroup.label", "Node group")}
                  placeholder={t(
                    "createOtaJobPage.nodeGroup.placeholder",
                    "Select a node group",
                  )}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      )}

      {isGroup && (
        <SelectableCardList
          allowMultiple
          element="switch"
          data={continuousCards}
          value={isContinuous ? [CONTINUOUS_CARD_ID] : []}
          onChange={handleContinuousChange}
          aria-label={t("createOtaJobPage.continuous.label", "Continuous")}
        />
      )}

      {showSource && (
        <SelectableCardList
          data={sourceCards}
          value={source}
          onChange={handleSourceChange}
          aria-label={t("createOtaJobPage.source.label", "Source")}
        />
      )}

      {showRules && <TargetRulesField />}

      {!isGroup && (
        <FormField
          control={control}
          name="targetName"
          render={({ field }) => (
            <FormItem>
              <FormControl>
                <ThingSelector
                  value={field.value || undefined}
                  onSelect={(next) => field.onChange(next ?? "")}
                  onError={handleTargetError}
                  label={t("createOtaJobPage.node.label", "Node")}
                  placeholder={t(
                    "createOtaJobPage.node.placeholder",
                    "Select a node",
                  )}
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
