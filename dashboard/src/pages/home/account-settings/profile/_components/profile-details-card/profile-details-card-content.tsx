/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  DynamicList,
  NoDataCard,
} from "@espressif/dashboard-ui-components/components";
import type { ProfileDetailsCardContentProps } from "./profile-details-card-content.props";

export default function ProfileDetailsCardContent({
  items,
  meta,
}: ProfileDetailsCardContentProps) {
  const { t } = useTranslation("account-settings");

  if (items.length === 0) {
    return (
      <NoDataCard
        heading={t(
          "profile.details.empty.heading",
          "Profile details unavailable",
        )}
        description={t(
          "profile.details.empty.description",
          "We couldn't read your session information. Sign in again to refresh it.",
        )}
      />
    );
  }

  return (
    <DynamicList
      items={items}
      meta={meta}
      direction="row"
      hideIcon
      simple
      keyWidth={35}
      useIconsForBoolean
    />
  );
}
