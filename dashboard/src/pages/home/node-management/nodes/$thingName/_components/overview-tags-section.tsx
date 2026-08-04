/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  ArrowRight,
  Cpu,
  Settings,
  ShieldCheck,
  Tag as TagIcon,
  User,
} from "lucide-react";
import {
  Alert,
  Badge,
  Button,
  SectionCard,
  Skeleton,
  Tag,
} from "@espressif/dashboard-ui-components/components";
import { useNodeTags } from "@/api/node-tags";

interface OverviewTagsSectionProps {
  thingName: string;
}

interface TagGroupProps {
  title: string;
  icon: ReactNode;
  entries: [string, string][];
  emptyMessage: string;
}

function TagGroup({ title, icon, entries, emptyMessage }: TagGroupProps) {
  return (
    <SectionCard
      size="sm"
      icon={icon}
      primaryText={
        <span className="flex items-center gap-2">
          {title}
          <Badge color="secondary" variant="solid" className="font-normal rounded-full px-1.5 py-0.5 text-xs">
            {entries.length}
          </Badge>
        </span>
      }
      defaultOpen={false}
      color="silver"
      variant="soft"
    >
      {entries.length === 0 ? (
        <Alert variant="soft" color="info" type="info" hideIcon>
          {emptyMessage}
        </Alert>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          {entries.map(([key, value]) => (
            <Tag
              key={key}
              name={key}
              value={value}
              color="secondary"
              variant="outline"
              className="px-2 py-1"
              size="sm"
              rounded
            />
          ))}
        </div>
      )}
    </SectionCard>
  );
}

function sortedEntries(record: Record<string, string> | undefined): [string, string][] {
  return Object.entries(record ?? {}).sort(([a], [b]) =>
    a.localeCompare(b, undefined, { sensitivity: "base" }),
  );
}

export default function OverviewTagsSection({ thingName }: OverviewTagsSectionProps) {
  const { t } = useTranslation("nodes");
  const navigate = useNavigate();

  const { data, isLoading, isError } = useNodeTags(thingName);

  const body = (() => {
    if (isLoading) {
      return (
        <div className="flex flex-col gap-3">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-20 w-full" />
        </div>
      );
    }
    if (isError) {
      return (
        <Alert variant="soft" color="error" type="error">
          {t("details.overview.tagsError", "Failed to load tags")}
        </Alert>
      );
    }
    return (
      <div className="flex flex-col gap-3">
        <TagGroup
          title={t("details.overview.adminTags", "Admin Tags")}
          icon={<ShieldCheck className="h-4 w-4" />}
          entries={sortedEntries(data?.admin)}
          emptyMessage={t("details.overview.noAdminTags", "No admin tags")}
        />
        <TagGroup
          title={t("details.overview.userTags", "User Tags")}
          icon={<User className="h-4 w-4" />}
          entries={sortedEntries(data?.user)}
          emptyMessage={t("details.overview.noUserTags", "No user tags")}
        />
        <TagGroup
          title={t("details.overview.deviceTags", "Device Tags")}
          icon={<Cpu className="h-4 w-4" />}
          entries={sortedEntries(data?.device)}
          emptyMessage={t("details.overview.noDeviceTags", "No device tags")}
        />
      </div>
    );
  })();

  return (
    <SectionCard
      icon={<TagIcon className="h-6 w-6" />}
      primaryText={t("details.overview.tagsSection", "Tags")}
      color="silver"
      variant="outline"
      actions={
        <Button
          type="button"
          variant="outline"
          size="sm"
          color="gray"
          usePrimaryColorOnHover
          startIcon={<Settings className="h-4 w-4" />}
          endIcon={<ArrowRight className="h-4 w-4" />}
          onClick={() =>
            void navigate({
              to: "/home/node-management/nodes/$thingName/tags",
              params: { thingName },
            })
          }
        >
          {t("details.overview.manageAllTags", "Manage all tags")}
        </Button>
      }
    >
      {body}
    </SectionCard>
  );
}
