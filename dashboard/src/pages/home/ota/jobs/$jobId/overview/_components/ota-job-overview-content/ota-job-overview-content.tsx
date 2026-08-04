/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  SectionCard,
  ContentContainer,
} from "@espressif/dashboard-ui-components/components";
import OtaJobActivityCard from "../ota-job-activity-card/ota-job-activity-card";
import OtaJobCompletionSummaryCard from "../ota-job-completion-summary-card/ota-job-completion-summary-card";
import OtaJobTargetCard from "../ota-job-target-card/ota-job-target-card";
import type { OtaJobOverviewContentProps } from "./ota-job-overview-content.props";

export default function OtaJobOverviewContent({
  job,
}: OtaJobOverviewContentProps) {
  return (
    <ContentContainer maxWidth="xl" noGutters>
      <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-12">
        <div className="lg:col-span-8">
          <SectionCard
            size="lg"
            variant="outline"
            color="mist"
            allowCollapse={false}
          >
            <div className="flex flex-col gap-4 lg:gap-6">
              <OtaJobTargetCard job={job} />
              <OtaJobActivityCard job={job} />
            </div>
          </SectionCard>
        </div>
        <div className="lg:col-span-4">
          <OtaJobCompletionSummaryCard job={job} />
        </div>
      </div>
    </ContentContainer>
  );
}
