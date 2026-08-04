/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";

import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronsRight, ChevronUp } from "lucide-react";
import {
  Button,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Link,
  ProgressBar,
  Typography,
} from "@espressif/dashboard-ui-components/components";
import {
  getOtaJobStatusCounts,
  getOtaJobStatusPresentation,
} from "@/config/ota-job-status.config";
import { getPresetColorTextClass } from "@/config/node-status.config";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import type { OtaJobCompletionSummaryCardProps } from "./ota-job-completion-summary-card.props";
import { useRouteParams } from "@/lib/navigation/use-route-params";
import {
  getOtaJobCompletionPercent,
  getOtaJobCompletionTotals,
} from "./ota-job-completion-summary-card.utils";

export default function OtaJobCompletionSummaryCard({
  job,
}: OtaJobCompletionSummaryCardProps) {
  const { t } = useTranslation("ota-jobs");
  const params = useRouteParams<{ jobId?: string }>();
  const jobId = params.jobId ?? "";
  const [viewMore, setViewMore] = useState(false);

  const { succeeded, total } = getOtaJobCompletionTotals(job.jobProcessDetails);
  const percent = getOtaJobCompletionPercent(job.jobProcessDetails);

  // Every status other than the headline "Succeeded" row lives under "View more"
  // so the full breakdown is always reachable, even when the counts are zero.
  const extraCounts = getOtaJobStatusCounts(job.jobProcessDetails).filter(
    ({ statusKey }) => statusKey !== "SUCCEEDED",
  );

  return (
    <div className="flex w-full flex-col gap-4 rounded-xl border border-primary/10 bg-gradient-to-b from-primary/5 to-primary/10 p-4">
      <div className="flex items-start justify-between gap-3">
        <Typography variant="h3" as="div" className="text-foreground">
          {t("details.overview.completionSummary.heading", "Status summary")}
        </Typography>
        <Link
          to="/home/ota/jobs/$jobId/nodes"
          params={{ jobId }}
          linkComponent={TanstackRouterLink}
          color="primary"
          endIcon={<ChevronsRight className="h-4 w-4 shrink-0" aria-hidden />}
        >
          {t("details.overview.completionSummary.details", "Details")}
        </Link>
      </div>

      <ProgressBar
        className="w-full"
        value={percent}
        color="success"
        showPercentage
        label={t(
          "details.overview.completionSummary.progressLabel",
          "Succeeded nodes {{succeeded}}/{{total}}",
          { succeeded, total },
        )}
      />

      <div className="flex flex-col gap-3">
        <div className="flex items-center justify-between gap-3 text-sm font-semibold text-foreground">
          <span>{t("details.overview.completionSummary.total", "Total")}</span>
          <span className="tabular-nums">{total}</span>
        </div>
        <div className="flex items-center justify-between gap-3 text-sm font-semibold text-foreground">
          <span>
            {t("details.overview.completionSummary.succeeded", "Succeeded")}
          </span>
          <span className="tabular-nums">{succeeded}</span>
        </div>

        {extraCounts.length > 0 ? (
          <Collapsible open={viewMore} onOpenChange={setViewMore}>
            <CollapsibleContent className="data-[state=closed]:animate-none">
              <div className="flex flex-col gap-3 border-t border-primary/10 pt-3">
                {extraCounts.map(({ statusKey, count }) => {
                  const { Icon, color, i18nKey } =
                    getOtaJobStatusPresentation(statusKey);
                  return (
                    <div
                      key={statusKey}
                      className="flex items-center justify-between gap-3 text-sm text-muted-foreground"
                    >
                      <span className="flex min-w-0 items-center gap-2">
                        <Icon
                          className={`h-4 w-4 shrink-0 ${getPresetColorTextClass(color)}`}
                          aria-hidden
                        />
                        <span className="truncate">
                          {i18nKey ? t(i18nKey, statusKey) : statusKey}
                        </span>
                      </span>
                      <span className="tabular-nums">{count}</span>
                    </div>
                  );
                })}
              </div>
            </CollapsibleContent>
            <div className="flex justify-end pt-1">
              <CollapsibleTrigger asChild>
                <Button
                  type="button"
                  variant="link"
                  color="gray"
                  size="sm"
                  fullWidth={false}
                  className="p-0"
                  endIcon={
                    viewMore ? (
                      <ChevronUp className="h-4 w-4 shrink-0" aria-hidden />
                    ) : (
                      <ChevronDown className="h-4 w-4 shrink-0" aria-hidden />
                    )
                  }
                >
                  {viewMore
                    ? t("details.overview.completionSummary.viewLess", "View less")
                    : t("details.overview.completionSummary.viewMore", "View more")}
                </Button>
              </CollapsibleTrigger>
            </div>
          </Collapsible>
        ) : null}
      </div>
    </div>
  );
}
