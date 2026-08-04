/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Boxes } from "lucide-react";
import { SectionCard } from "@espressif/dashboard-ui-components/components";
import OtaNodeExecutionDetailSheet from "../ota-node-execution-detail-sheet/ota-node-execution-detail-sheet";
import OtaJobNodeExecutionsMainContent from "./_components/ota-job-node-executions-main-content";
import type { OtaJobNodeExecutionsCardProps } from "./ota-job-node-executions-card.props";

export default function OtaJobNodeExecutionsCard({
  jobId,
}: OtaJobNodeExecutionsCardProps) {
  const { t } = useTranslation("ota-jobs");
  const [selectedThingName, setSelectedThingName] = useState<string | null>(
    null,
  );

  return (
    <SectionCard
      variant="outline"
      color="silver"
      allowCollapse={false}
      icon={<Boxes className="h-4 w-4" />}
      primaryText={t("details.nodes.executions.title", "Node executions")}
      secondaryText={t(
        "details.nodes.executions.subtitle",
        "Per-node OTA delivery status for this job.",
      )}
    >
      <OtaJobNodeExecutionsMainContent
        jobId={jobId}
        onSelectNode={setSelectedThingName}
      />
      {selectedThingName !== null && (
        <OtaNodeExecutionDetailSheet
          jobId={jobId}
          thingName={selectedThingName}
          onClose={() => setSelectedThingName(null)}
        />
      )}
    </SectionCard>
  );
}
