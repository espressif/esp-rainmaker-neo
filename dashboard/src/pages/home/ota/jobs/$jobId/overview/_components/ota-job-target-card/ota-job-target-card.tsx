/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  Alert,
  Badge,
  SectionCard,
  SimpleClickableCard,
} from "@espressif/dashboard-ui-components/components";
import { Crosshair } from "lucide-react";
import {
  OTA_TARGET_TYPE_PRESENTATION,
  parseOtaTarget,
  type OtaTarget,
} from "@/config/ota-target.config";
import type { OtaJobTargetCardProps } from "./ota-job-target-card.props";
import TargetSelectionBadge from "./target-selection-badge";

export default function OtaJobTargetCard({ job }: OtaJobTargetCardProps) {
  const { t } = useTranslation("ota-jobs");
  const navigate = useNavigate();

  const targets = useMemo(
    () => (job.targets ?? []).map(parseOtaTarget),
    [job.targets],
  );

  const handleTargetClick = useCallback(
    (target: OtaTarget) => {
      if (target.type === "thinggroup") {
        void navigate({
          to: "/home/node-management/node-groups/$groupName",
          params: { groupName: target.name },
        });
        return;
      }
      void navigate({
        to: "/home/node-management/nodes/$thingName",
        params: { thingName: target.name },
      });
    },
    [navigate],
  );

  return (
    <SectionCard
      variant="soft"
      color="silver"
      allowCollapse={false}
      icon={<Crosshair className="h-4 w-4" />}
      primaryText={t("details.overview.target.title", "Target")}
      secondaryText={t(
        "details.overview.target.description",
        "The target of the OTA job.",
      )}
      actions={<TargetSelectionBadge selection={job.targetSelection} />}
    >
      {targets.length === 0 ? (
        <Alert variant="soft" color="info" type="info" hideIcon>
          {t("details.overview.target.empty", "This job has no targets.")}
        </Alert>
      ) : (
        <div className="flex flex-col gap-2">
          {targets.map((target) => {
            const { Icon, i18nKey, labelFallback } =
              OTA_TARGET_TYPE_PRESENTATION[target.type];
            return (
              <SimpleClickableCard
                key={target.arn}
                size="sm"
                variant="soft"
                color="gray"
                icon={<Icon className="h-4 w-4" />}
                title={target.name}
                description={
                  <Badge color="primary" variant="soft" className="font-normal border border-solid border-primary/20">
                    {t(i18nKey, labelFallback)}
                  </Badge>
                }
                truncateTitle
                onClick={() => handleTargetClick(target)}
              />
            );
          })}
        </div>
      )}
    </SectionCard>
  );
}
