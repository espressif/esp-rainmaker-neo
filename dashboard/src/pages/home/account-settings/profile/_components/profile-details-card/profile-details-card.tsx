/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { IdCard } from "lucide-react";
import { SectionCard } from "@espressif/dashboard-ui-components/components";
import ProfileDetailsCardContent from "./profile-details-card-content";
import { buildProfileItems, buildProfileMeta } from "./profile-details-card.utils";
import type { ProfileDetailsCardProps } from "./profile-details-card.props";

export default function ProfileDetailsCard({ claims }: ProfileDetailsCardProps) {
  const { t } = useTranslation("account-settings");

  const items = useMemo(
    () => (claims ? buildProfileItems(claims) : []),
    [claims],
  );
  const meta = useMemo(() => buildProfileMeta(t), [t]);

  return (
    <SectionCard
      icon={<IdCard className="h-5 w-5" />}
      primaryText={t("profile.details.heading", "Profile details")}
      secondaryText={t(
        "profile.details.secondaryText",
        "Identity information from your current sign-in session.",
      )}
      allowCollapse={false}
      color="silver"
      variant="soft"
      size="lg"
    >
      <ProfileDetailsCardContent items={items} meta={meta} />
    </SectionCard>
  );
}
