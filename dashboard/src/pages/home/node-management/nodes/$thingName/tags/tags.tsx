/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Cpu, ShieldCheck, User } from "lucide-react";
import {
  ContentContainer,
  ScrollableSections,
} from "@espressif/dashboard-ui-components/components";
import AdminTagsSection from "./_components/admin-tags-section";
import DeviceTagsSection from "./_components/device-tags-section";
import UserTagsSection from "./_components/user-tags-section";
import { useRouteParams } from "@/lib/navigation/use-route-params";

const SECTIONS = {
  admin: "admin",
  user: "user",
  device: "device",
} as const;

export default function ThingTagsPage() {
  const { t } = useTranslation("nodes");
  const params = useRouteParams<{ thingName?: string }>();
  const thingName = params.thingName;

  if (!thingName) {
    return null;
  }

  return (
    <ContentContainer maxWidth="xl" noGutters>
      <ScrollableSections defaultValue={SECTIONS.admin} stickyTop="15rem">
        <ScrollableSections.Tabs>
          <ScrollableSections.Tab
            id={SECTIONS.admin}
            label={t("tags.sections.admin.title", "Admin tags")}
          >
            <ShieldCheck className="h-4 w-4 shrink-0" aria-hidden />
            <span>{t("tags.sections.admin.title", "Admin tags")}</span>
          </ScrollableSections.Tab>
          <ScrollableSections.Tab
            id={SECTIONS.user}
            label={t("tags.sections.user.title", "User tags")}
          >
            <User className="h-4 w-4 shrink-0" aria-hidden />
            <span>{t("tags.sections.user.title", "User tags")}</span>
          </ScrollableSections.Tab>
          <ScrollableSections.Tab
            id={SECTIONS.device}
            label={t("tags.sections.device.title", "Device tags")}
          >
            <Cpu className="h-4 w-4 shrink-0" aria-hidden />
            <span>{t("tags.sections.device.title", "Device tags")}</span>
          </ScrollableSections.Tab>
        </ScrollableSections.Tabs>

        <ScrollableSections.Content id={SECTIONS.admin}>
          <AdminTagsSection thingName={thingName} />
        </ScrollableSections.Content>
        <ScrollableSections.Content id={SECTIONS.user}>
          <UserTagsSection thingName={thingName} />
        </ScrollableSections.Content>
        <ScrollableSections.Content id={SECTIONS.device}>
          <DeviceTagsSection thingName={thingName} />
        </ScrollableSections.Content>
      </ScrollableSections>
    </ContentContainer>
  );
}
