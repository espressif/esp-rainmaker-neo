/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown } from "lucide-react";
import {
  Button,
  CircularProgress,
  Popover,
  PopoverContent,
  PopoverTrigger,
  ProgressBar,
} from "@espressif/dashboard-ui-components/components";
import { useGetQuota } from "@/api/license";
import { getClaimedQuotaMetrics } from "../../_utils/claimed-quota-metrics";

export function NodesQuotaDetails() {
  const { t } = useTranslation("nodes");
  const [open, setOpen] = useState(false);
  const { data, isError } = useGetQuota();

  if (isError || !data) {
    return null;
  }

  const metrics = getClaimedQuotaMetrics(data.used, data.total_limit);
  if (metrics == null) {
    return null;
  }

  const {
    progressValue,
    percentRounded,
    color,
    total: totalValue,
    quota: quotaValue,
  } = metrics;

  const triggerLabel = t("quotaDetails.percentClaimed", "{{percent}}% Claimed", {
    percent: percentRounded,
    defaultValue: "{{percent}}% Claimed",
  });

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          color="gray"
          fullWidth={false}
          endIcon={<ChevronDown className="h-4 w-4 shrink-0" />}
          className="max-w-[min(100%,14rem)]"
          type="button"
          usePrimaryColorOnHover
          title={t("totalClaimedNodes", "{{total}} / {{quota}} nodes registered", {
            total: totalValue,
            quota: quotaValue,
            defaultValue: "{{total}} / {{quota}} nodes registered",
          })}
        >
          <span className="flex min-w-0 items-center gap-2">
            <CircularProgress
              value={progressValue}
              color={color}
              size={16}
              showPercentage={false}
              className="shrink-0"
            />
            <span className="min-w-0 truncate">{triggerLabel}</span>
          </span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80">
        <div className="flex flex-col gap-3">
          <p>
            <span className="text-3xl font-bold tabular-nums pr-2">
              {t("quotaDetails.percentValue", "{{percent}}%", {
                percent: percentRounded,
                defaultValue: "{{percent}}%",
              })}
            </span>
            <span className="text-lg font-medium text-muted-foreground tracking-normal">
              {t("quotaDetails.nodesClaimed", "nodes claimed")}
            </span>
          </p>
          <ProgressBar
            value={progressValue}
            showFakeProgress={false}
            color={color}
            className="w-full"
          />
          <div className="flex flex-col gap-2 text-sm">
            <div className="flex items-center justify-between gap-3 border-b pb-2">
              <span className="font-normal">
                {t("quotaDetails.quotaLabel", "Quota")}
              </span>
              <span className="tabular-nums text-muted-foreground">
                {quotaValue.toLocaleString()}
              </span>
            </div>
            <div className="flex items-center justify-between gap-3">
              <span className="font-normal">
                {t("quotaDetails.claimedLabel", "Claimed")}
              </span>
              <span className="tabular-nums text-muted-foreground">
                {totalValue.toLocaleString()}
              </span>
            </div>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
