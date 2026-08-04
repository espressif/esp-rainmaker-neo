/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Group } from "lucide-react";
import { Button } from "@espressif/dashboard-ui-components/components";
import { OtaJobGroupFilterDialog } from "./_components/ota-job-group-filter-dialog";
import type { OtaJobGroupFilterProps } from "./ota-job-group-filter.props";

/**
 * Node-group filter for the OTA jobs toolbar. The trigger is a plain button
 * styled like the sibling status/target filters; clicking it opens a modal
 * dialog wrapping the shared `ThingGroupSelector`. The selection is edited on a
 * local draft and only committed on "Apply", so cancelling leaves the applied
 * filter untouched.
 */
export function OtaJobGroupFilter({ value, onChange }: OtaJobGroupFilterProps) {
  const { t } = useTranslation("ota-jobs");
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState(value);

  // Re-sync the draft to the applied value each time the dialog opens so a
  // previous cancelled edit never leaks into the next session.
  const handleOpen = () => {
    setDraft(value);
    setOpen(true);
  };

  const handleApply = () => {
    onChange(draft);
    setOpen(false);
  };

  const handleCancel = () => {
    setOpen(false);
  };

  return (
    <>
      <span className="relative inline-flex">
        <Button
          variant="outline"
          color="gray"
          size="sm"
          usePrimaryColorOnHover
          fullWidth={false}
          type="button"
          tooltip={value || undefined}
          startIcon={<Group className="h-3.5 w-3.5 shrink-0" />}
          onClick={handleOpen}
        >
          {t("filters.groupLabel", "Node group")}
        </Button>
        {value && (
          <span
            aria-hidden
            className="absolute -right-1 -top-1 h-2 w-2 rounded-full bg-destructive ring-2 ring-background"
          />
        )}
      </span>

      <OtaJobGroupFilterDialog
        open={open}
        onOpenChange={setOpen}
        value={draft}
        onValueChange={setDraft}
        onApply={handleApply}
        onCancel={handleCancel}
      />
    </>
  );
}
