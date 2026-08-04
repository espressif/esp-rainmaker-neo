/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, X } from "lucide-react";
import { Alert } from "@espressif/dashboard-ui-components/components";
import { FormFooterActions } from "@/components/form-footer-actions";
import { ThingGroupSelector } from "@/aws/components/thing-group-selector";
import type { OtaJobGroupFilterBodyProps } from "./ota-job-group-filter-body.props";

/**
 * Body of the node-group filter dialog: the shared `ThingGroupSelector` plus
 * Cancel/Apply. Mounts and unmounts with the dialog content, so the load-failure
 * banner is scoped to a single open session and clears on the next one.
 */
export function OtaJobGroupFilterBody({
  value,
  onValueChange,
  onApply,
  onCancel,
}: OtaJobGroupFilterBodyProps) {
  const { t } = useTranslation(["ota-jobs", "common"]);
  const [loadFailed, setLoadFailed] = useState(false);

  const handleGroupError = () => {
    setLoadFailed(true);
  };

  const handleGroupSelect = (groupName: string | undefined) => {
    setLoadFailed(false);
    onValueChange(groupName);
  };

  return (
    <div className="flex flex-col gap-4">
      {loadFailed && (
        <Alert type="error" variant="soft" color="error">
          {t(
            "filters.groupLoadError",
            "Could not load node groups. Check your connection and try again.",
          )}
        </Alert>
      )}

      <ThingGroupSelector
        value={value}
        onSelect={handleGroupSelect}
        onError={handleGroupError}
        label={null}
        searchPlaceholder={t("filters.groupSearchPlaceholder", "Search groups…")}
        emptyText={t("filters.groupEmptyText", "No groups found")}
      />

      <FormFooterActions
        softAction={{
          label: t("common:actions.cancel", "Cancel"),
          startIcon: <X className="h-4 w-4 shrink-0" />,
          onClick: onCancel,
        }}
        primaryAction={{
          label: t("filters.groupModalApply", "Apply"),
          startIcon: <Check className="h-4 w-4 shrink-0" />,
          onClick: onApply,
        }}
      />
    </div>
  );
}
