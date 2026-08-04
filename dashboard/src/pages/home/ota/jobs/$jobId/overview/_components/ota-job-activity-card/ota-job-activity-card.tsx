/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import type { Job } from "@aws-sdk/client-iot";
import {
  SectionCard,
  SimpleList,
  SimplifiedDate,
  type SimpleListItem,
} from "@espressif/dashboard-ui-components/components";
import { History } from "lucide-react";
import type { OtaJobActivityCardProps } from "./ota-job-activity-card.props";

/**
 * One row per timestamp. A missing date leaves `content` undefined so
 * `SimpleList` auto-hides the row (e.g. `completedAt` for a running job).
 * Dates render absolute (no relative "x ago") — `SimplifiedDate` is absolute
 * by default.
 */
function buildActivityItems(job: Job, t: TFunction): SimpleListItem[] {
  const rows: { key: string; label: string; date?: Date }[] = [
    {
      key: "createdAt",
      label: t("common:created", "Created"),
      date: job.createdAt,
    },
    {
      key: "lastUpdatedAt",
      label: t("details.overview.activity.lastUpdatedAt", "Last updated"),
      date: job.lastUpdatedAt,
    },
    {
      key: "completedAt",
      label: t("details.overview.activity.completedAt", "Completed"),
      date: job.completedAt,
    },
  ];

  return rows.map(({ key, label, date }) => ({
    key,
    label,
    direction: "row",
    content: date ? <SimplifiedDate ts={date.getTime()} /> : undefined,
  }));
}

export default function OtaJobActivityCard({ job }: OtaJobActivityCardProps) {
  const { t } = useTranslation(["ota-jobs", "common"]);
  const items = useMemo(() => buildActivityItems(job, t), [job, t]);

  return (
    <SectionCard
      variant="soft"
      color="silver"
      allowCollapse={false}
      icon={<History className="h-4 w-4" />}
      primaryText={t("details.overview.activity.title", "Activity")}
      secondaryText={t(
        "details.overview.activity.description",
        "Timeline of this OTA job.",
      )}
    >
      <SimpleList items={items} size="sm" />
    </SectionCard>
  );
}
