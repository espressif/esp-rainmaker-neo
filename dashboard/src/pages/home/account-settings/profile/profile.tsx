/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { useUserStore } from "@/stores/user.store";
import { ProfileDetailsCard } from "./_components/profile-details-card";
import { UserProfileCard } from "./_components/user-profile-card";
import { useProfileClaims } from "./_hooks/use-profile-claims";

/**
 * Profile tab. Identity comes from the Cognito id_token claims (decoded
 * locally — AWS provides no profile API for admin users), with the persisted
 * login username as fallback so the card renders even without a token.
 *
 * The section shell (`account-settings.tsx`) supplies `PageContainer` and the
 * page heading, so tab bodies render bare cards.
 */
export default function Profile() {
  const { t } = useTranslation("account-settings");
  const loggedInUserName = useUserStore((s) => s.loggedInUserName);
  const claims = useProfileClaims();

  const email =
    claims?.email ?? loggedInUserName ?? t("profile.unknownUser", "Unknown user");
  const username = claims?.["cognito:username"] ?? loggedInUserName;

  return (
    <div className="flex flex-col gap-6">
      <UserProfileCard email={email} username={username} />
      <ProfileDetailsCard claims={claims} />
    </div>
  );
}
