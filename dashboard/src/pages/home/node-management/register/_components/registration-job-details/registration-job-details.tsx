/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { Group } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  Accordion,
  CopiableText,
  DynamicList,
  SectionCard,
  type AccordionItem,
  type DynamicListEntry,
  type DynamicListMetaEntry,
} from "@espressif/dashboard-ui-components/components";
import { RegistrationJobAvatar } from "@/components/avatars/registration-job-avatar";
import { GroupList } from "@/components/groups-card";
import { getRegistrationJobFileName } from "@/lib/registration-jobs/registration-job-display";
import { RegistrationJobDownloadButton } from "../registration-job-download-button";
import { RegistrationJobStatusBadge } from "../registration-job-status-badge";
import type { RegistrationJobDetailsProps } from "./registration-job-details.props";

const META: Record<string, DynamicListMetaEntry> = {
  request_id: { type: "mono" },
  user_id: { type: "mono" },
  job_type: { type: "badge" },
  created_at: { type: "timestamp" },
  last_updated_at: { type: "timestamp" },
  message: { type: "info" },
  cert_file_s3_path: { type: "mono" },
  failed_file_s3_path: { type: "mono" },
};

export function RegistrationJobDetails({
  job,
  onDownload,
}: RegistrationJobDetailsProps) {
  const { t } = useTranslation("register");

  const fileName =
    getRegistrationJobFileName(job.cert_file_s3_path) ??
    t("unknownFile", "Registration job");

  const items = useMemo<DynamicListEntry[]>(() => {
    const entries: DynamicListEntry[] = [
      { key: "request_id", value: job.request_id },
      { key: "job_type", value: job.job_type ?? "register" },
      { key: "total_nodes", value: job.total_nodes ?? 0 },
      { key: "success_count", value: job.success_count ?? 0 },
      { key: "failed_count", value: job.failed_count ?? 0 },
      { key: "created_at", value: job.created_at },
      { key: "last_updated_at", value: job.last_updated_at },
    ];

    if (job.admin_parent_group_name) {
      entries.push({
        key: "admin_parent_group_name",
        value: job.admin_parent_group_name,
      });
    }
    if (job.tags && job.tags.length > 0) {
      entries.push({ key: "tags", value: job.tags });
    }
    if (job.message) {
      entries.push({ key: "message", value: job.message });
    }
    if (job.cert_file_s3_path) {
      entries.push({ key: "cert_file_s3_path", value: job.cert_file_s3_path });
    }
    if (job.failed_file_s3_path) {
      entries.push({
        key: "failed_file_s3_path",
        value: job.failed_file_s3_path,
      });
    }
    if (job.user_id) {
      entries.push({ key: "user_id", value: job.user_id });
    }

    return entries;
  }, [job]);

  const accordionItems: AccordionItem[] = useMemo(
    () => [
      {
        id: "admin-groups",
        title: t("details.adminGroups", "Admin groups"),
        icon: <Group className="h-4 w-4" aria-hidden />,
        content: (
          <GroupList
            groupNames={job.admin_group_names ?? []}
            emptyText={t(
              "details.noAdminGroups",
              "No admin groups",
            )}
          />
        ),
      },
    ],
    [t, job.admin_group_names],
  );

  return (
    <SectionCard
      icon={<RegistrationJobAvatar failedCount={job.failed_count ?? 0} />}
      primaryText={
        <div className="flex items-center gap-2">
          <span>{fileName}</span>
          <RegistrationJobStatusBadge status={job.status} />
        </div>
      }
      secondaryText={
        <CopiableText
          text={job.request_id}
          className="text-xs text-muted-foreground"
        />
      }
      actions={
        <RegistrationJobDownloadButton
          certFileS3Path={job.cert_file_s3_path}
          onDownload={onDownload}
        />
      }
      variant="outline"
      color="mist"
      allowCollapse={false}
      className="mt-5"
    >
      <div className="flex flex-col gap-4">
        <DynamicList
          items={items}
          meta={META}
          direction="row"
          keyWidth={35}
          hideIcon
          simple
        />
        <SectionCard variant="outline" color="mist" allowCollapse={false} size="sm">
          <Accordion items={accordionItems} size="sm" />
        </SectionCard>
      </div>
    </SectionCard>
  );
}
