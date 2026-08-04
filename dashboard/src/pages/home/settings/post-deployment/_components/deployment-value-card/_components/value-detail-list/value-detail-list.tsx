/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Activity, Rocket } from "lucide-react";
import {
  SimpleList,
  Typography,
  type SimpleListItem,
  type SimpleListItemDirection,
} from "@espressif/dashboard-ui-components/components";
import ValueReadingContent from "../value-reading";
import type { ValueDetailListProps } from "./value-detail-list.props";

/**
 * The reading itself plus the guidance on raising it. The guidance is a list row
 * rather than the card's `secondaryText` because `SectionCard` truncates that to a
 * single line, and the guidance is the actionable part of this page.
 */
export default function ValueDetailList({
  reading,
  noteKey,
}: ValueDetailListProps) {
  const { t } = useTranslation("post-deployment");
  const note = t(noteKey, "");

  // Measurements drop to their own row: they run long (and longer once translated),
  // so squeezing them right-aligned beside the label truncates them. A status badge
  // is short, so it stays inline.
  const readingDirection: SimpleListItemDirection =
    reading.display === "text" ? "column" : "row";

  const items = useMemo<SimpleListItem[]>(
    () => [
      {
        key: "current",
        label: t("currentValue", "Current"),
        icon: Activity,
        direction: readingDirection,
        content: <ValueReadingContent reading={reading} />,
      },
      {
        key: "guidance",
        label: t("raisingThisLimit", "Raising this limit"),
        icon: Rocket,
        // `hideIfEmpty` only skips null/undefined, so a blank note has to become
        // undefined or it renders as an empty row.
        content: note ? (
          <Typography variant="body2" as="p" className="text-foreground">
            {note}
          </Typography>
        ) : undefined,
      },
    ],
    [note, reading, readingDirection, t],
  );

  return <SimpleList items={items} size="sm" />;
}
