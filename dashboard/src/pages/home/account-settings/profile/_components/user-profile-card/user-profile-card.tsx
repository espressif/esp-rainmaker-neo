/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { UserRound } from "lucide-react";
import {
  IconAvatar,
  SectionCard,
  Typography,
  CopiableText,
} from "@espressif/dashboard-ui-components/components";
import type { UserProfileCardProps } from "./user-profile-card.props";

export default function UserProfileCard({
  email,
  username,
  className,
}: UserProfileCardProps) {
  const showUsername = !!username && username !== email;

  return (
    <SectionCard
      allowCollapse={false}
      variant="gradient"
      color="primary"
      className={className}
    >
      <div className="flex flex-col min-w-0 items-center p-3 py-6 gap-5">
        <div className="shrink-0">
          <IconAvatar size={100} color="info" ring={{show: true, color: "info"}}>
            <UserRound size={56} />
          </IconAvatar>
        </div>
        <div className="flex w-full min-w-0 flex-col items-center gap-1.5">
          <Typography
            variant="h3"
            className="w-full truncate text-center text-foreground"
          >
            {email}
          </Typography>
          {showUsername && (
            <CopiableText
              className="truncate text-center text-muted-foreground text-sm"
              text={username}
            />
          )}
        </div>
      </div>
    </SectionCard>
  );
}
