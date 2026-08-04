/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { FileJson, PencilLine } from "lucide-react";
import {
  SelectableCardList,
  Typography,
} from "@espressif/dashboard-ui-components/components";
import type { SelectableCardListItem } from "@espressif/dashboard-ui-components/components";
import type { GvaEditMethod } from "../../use-gva-config-form";
import type { GvaEditMethodFieldProps } from "./gva-edit-method-field.props";

const ICON_CLASS = "h-5 w-5";

/**
 * Lets the user pick how to provide credentials: upload a service-account JSON
 * (default) or type each field manually. Single-select, UI-only state — not part
 * of the submitted payload.
 */
export default function GvaEditMethodField({
  value,
  onChange,
}: GvaEditMethodFieldProps) {
  const { t } = useTranslation("voice-assistants");

  const label = t(
    "gva.form.editMethod.label",
    "How do you want to enter credentials?",
  );

  const data = useMemo<SelectableCardListItem[]>(
    () => [
      {
        id: "upload",
        icon: <FileJson className={ICON_CLASS} aria-hidden />,
        primaryText: t("gva.form.editMethod.upload", "Upload JSON"),
        secondaryText: t(
          "gva.form.editMethod.uploadDescription",
          "Import a Google service account JSON file",
        ),
      },
      {
        id: "manual",
        icon: <PencilLine className={ICON_CLASS} aria-hidden />,
        primaryText: t("gva.form.editMethod.manual", "Manual Input"),
        secondaryText: t(
          "gva.form.editMethod.manualDescription",
          "Enter each credential field yourself",
        ),
      },
    ],
    [t],
  );

  return (
    <div className="flex flex-col gap-2">
      <Typography variant="body2" as="span" className="font-medium">
        {label}
      </Typography>
      <SelectableCardList
        data={data}
        allowMultiple={false}
        value={value}
        onChange={(next) => onChange(next as GvaEditMethod)}
        aria-label={label}
      />
    </div>
  );
}
