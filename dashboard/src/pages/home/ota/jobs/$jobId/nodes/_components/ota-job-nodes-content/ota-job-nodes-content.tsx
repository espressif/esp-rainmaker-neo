/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { ContentContainer } from "@espressif/dashboard-ui-components/components";
import OtaJobNodeExecutionsCard from "../ota-job-node-executions-card/ota-job-node-executions-card";
import OtaJobStatusSummaryCard from "../ota-job-status-summary-card/ota-job-status-summary-card";
import type { OtaJobNodesContentProps } from "./ota-job-nodes-content.props";

export default function OtaJobNodesContent({ job }: OtaJobNodesContentProps) {
  const jobId = job.jobId ?? "";

  return (
    <ContentContainer maxWidth="xl" noGutters>
      <div className="flex flex-col gap-6">
        <OtaJobStatusSummaryCard job={job} />
        <OtaJobNodeExecutionsCard jobId={jobId} />
      </div>
    </ContentContainer>
  );
}
