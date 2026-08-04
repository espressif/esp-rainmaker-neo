/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { BarChart3 } from "lucide-react";
import {
  Alert,
  BarChart,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import type { OtaJobStatusSummaryCardProps } from "./ota-job-status-summary-card.props";
import {
  buildOtaJobStatusChartConfig,
  buildOtaJobStatusChartRow,
  getOtaJobStatusTotal,
} from "./ota-job-status-summary-card.utils";

export default function OtaJobStatusSummaryCard({
  job,
}: OtaJobStatusSummaryCardProps) {
  const { t } = useTranslation("ota-jobs");
  const details = job.jobProcessDetails;

  const total = useMemo(() => getOtaJobStatusTotal(details), [details]);
  const chartData = useMemo(
    () => [buildOtaJobStatusChartRow(details)],
    [details],
  );
  const chartConfig = useMemo(
    () => buildOtaJobStatusChartConfig(details, t),
    [details, t],
  );

  return (
    <SectionCard
      variant="outline"
      color="silver"
      allowCollapse={false}
      icon={<BarChart3 className="h-4 w-4" />}
      primaryText={t("details.nodes.statusSummary.heading", "Status summary")}
      secondaryText={t(
        "details.nodes.statusSummary.subtitle",
        "Node counts by OTA delivery status for this job.",
      )}
    >
      {total === 0 ? (
        <Alert variant="soft" color="info" type="info" hideIcon>
          {t(
            "details.nodes.statusSummary.empty",
            "No node executions to summarize yet.",
          )}
        </Alert>
      ) : (
        <BarChart
          data={chartData}
          config={chartConfig}
          layout="vertical"
          stacked
          showTooltip
          showLegend
          hideXAxis
          hideYAxis
          barSize="sm"
          className="aspect-auto h-24 min-h-0 w-full min-w-0 max-w-none !justify-start"
          margin={{ left: 8, right: 16, top: 4, bottom: 4 }}
        />
      )}
    </SectionCard>
  );
}
