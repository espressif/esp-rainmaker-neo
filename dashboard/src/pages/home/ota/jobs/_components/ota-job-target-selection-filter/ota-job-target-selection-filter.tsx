/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { TargetSelection } from "@aws-sdk/client-iot";
import { Check, ChevronDown, Circle } from "lucide-react";
import {
  Button,
  List,
  Menu,
} from "@espressif/dashboard-ui-components/components";
import type { ListGroup } from "@espressif/dashboard-ui-components/components";
import { cn } from "@/utils/utils";
import { getPresetColorTextClass } from "@/config/node-status.config";
import {
  getTargetSelectionPresentation,
  TARGET_SELECTION_IDS,
} from "@/config/target-selection.config";
import type { OtaJobTargetSelectionFilterProps } from "./ota-job-target-selection-filter.props";

export function OtaJobTargetSelectionFilter({
  value,
  onChange,
}: OtaJobTargetSelectionFilterProps) {
  const { t } = useTranslation("ota-jobs");

  const selectedPresentation = getTargetSelectionPresentation(value);
  const selectedLabel = value
    ? t(selectedPresentation.i18nKey, selectedPresentation.labelFallback)
    : t("filters.targetSelectionTrigger", "Job Mode");

  const handleSelect = useCallback(
    (targetSelection: TargetSelection) => {
      onChange(targetSelection === value ? null : targetSelection);
    },
    [onChange, value],
  );

  const targetSelectionListItems: ListGroup[] = useMemo(
    () => [
      {
        id: "ota-job-target-selection-options",
        items: TARGET_SELECTION_IDS.map((targetSelection) => {
          const { color, i18nKey, labelFallback } =
            getTargetSelectionPresentation(targetSelection);
          const isSelected = targetSelection === value;
          return {
            id: targetSelection,
            label: t(i18nKey, labelFallback),
            startIcon: (
              <Circle
                className={cn(
                  "h-3.5 w-3.5 shrink-0",
                  getPresetColorTextClass(color),
                )}
                fill="currentColor"
                stroke="none"
              />
            ),
            endIcon: (
              <Check
                className={cn(
                  "h-4 w-4 shrink-0",
                  isSelected ? "opacity-100" : "opacity-0",
                )}
                aria-hidden
              />
            ),
            isSelected,
            onClick: () => handleSelect(targetSelection),
          };
        }),
      },
    ],
    [t, value, handleSelect],
  );

  return (
    <Menu
      trigger={
        <Button
          variant="outline"
          color="gray"
          size="sm"
          usePrimaryColorOnHover
          fullWidth={false}
          startIcon={
            <Circle
              className={cn(
                "h-3.5 w-3.5 shrink-0",
                value
                  ? getPresetColorTextClass(selectedPresentation.color)
                  : "text-muted-foreground",
              )}
              fill="currentColor"
              stroke="none"
            />
          }
          endIcon={<ChevronDown />}
        >
          {selectedLabel}
        </Button>
      }
      align="start"
    >
      <List
        items={targetSelectionListItems}
        role="dropdown-list"
        showSeparators={false}
      />
    </Menu>
  );
}
