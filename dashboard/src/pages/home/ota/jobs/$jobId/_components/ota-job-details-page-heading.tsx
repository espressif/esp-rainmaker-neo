/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import type { Job } from "@aws-sdk/client-iot";
import { SimplifiedDate } from "@espressif/dashboard-ui-components/components";
import { InternalPageHeader } from "@/components/internal-page-header";
import { ResourceArnPopover } from "@/components/resource-arn-popover";
import { ResourceCreatedAt } from "@/components/resource-created-at";
import { OtaJobAvatar } from "@/components/avatars/ota-job-avatar";
import { OtaJobStatusBadge } from "@/components/ota-job/ota-job-status-badge";
import { stripOtaPrefix } from "@/aws/services/ota.service";
import OtaJobDetailsPageTabs, {
  type OtaJobDetailsTab,
} from "./ota-job-details-page-tabs";

interface OtaJobDetailsPageHeadingProps {
  job: Job;
  activeTab: OtaJobDetailsTab;
  onTabChange: (value: OtaJobDetailsTab) => void;
}

export default function OtaJobDetailsPageHeading({
  job,
  activeTab,
  onTabChange,
}: OtaJobDetailsPageHeadingProps) {
  const { t } = useTranslation("ota-jobs");
  const jobId = job.jobId ?? "";

  return (
    <InternalPageHeader
      resourceLabel={t("details.jobIdLabel", "Job ID")}
      resourceId={jobId}
      metaEnd={<ResourceCreatedAt ts={job.createdAt?.getTime()} />}
      avatar={<OtaJobAvatar status={job.status} size={56} />}
      heading={stripOtaPrefix(jobId)}
      description={
        <span className="inline-flex items-center gap-1.5">
          <OtaJobStatusBadge status={job.status} />
          {job.lastUpdatedAt ? (
            <span className="text-muted-foreground">
              ({t("details.statusUpdatedPrefix", "updated")}{" "}
              <SimplifiedDate
                ts={job.lastUpdatedAt.getTime()}
                relative
                className="text-muted-foreground"
              />
              )
            </span>
          ) : null}
        </span>
      }
      actions={<ResourceArnPopover arn={job.jobArn} />}
      footer={
        <OtaJobDetailsPageTabs activeTab={activeTab} onTabChange={onTabChange} />
      }
    />
  );
}
