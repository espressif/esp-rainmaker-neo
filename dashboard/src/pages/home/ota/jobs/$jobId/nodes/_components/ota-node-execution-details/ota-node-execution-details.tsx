/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { SectionCard } from "@espressif/dashboard-ui-components/components";
import { useOtaJobExecutionDetailQuery } from "@/api/ota-jobs";
import { ThingDeviceAvatar } from "@/components/avatars/thing-device-avatar";
import { OtaJobStatusBadge } from "@/components/ota-job/ota-job-status-badge";
import OtaNodeExecutionDetailsBody from "./_components/ota-node-execution-details-body";
import type { OtaNodeExecutionDetailsProps } from "./ota-node-execution-details.props";

/**
 * Self-contained node-execution details card. Fetches its own data and owns its
 * header/states, so it can be dropped into a sheet, dialog, or popover without
 * any container-specific wiring.
 */
export default function OtaNodeExecutionDetails({
  jobId,
  thingName,
}: OtaNodeExecutionDetailsProps) {
  const { data, isPending, isError } = useOtaJobExecutionDetailQuery(
    jobId,
    thingName,
  );

  return (
    <SectionCard
      size="lg"
      variant="outline"
      color="mist"
      allowCollapse={false}
      icon={<ThingDeviceAvatar deviceType={null} online={null} size={40} />}
      primaryText={thingName}
      actions={data ? <OtaJobStatusBadge status={data.status} /> : undefined}
    >
      <OtaNodeExecutionDetailsBody
        jobId={jobId}
        thingName={thingName}
        execution={data ?? null}
        isPending={isPending}
        isError={isError}
      />
    </SectionCard>
  );
}
