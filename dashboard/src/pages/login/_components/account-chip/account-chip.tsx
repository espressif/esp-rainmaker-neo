/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { UserRound } from "lucide-react";
import { IconTextActionCard } from "@espressif/dashboard-ui-components/components";
import type { AccountChipProps } from "./account-chip.props";

/**
 * The account being signed in: avatar plus address, shared by the
 * remembered-account and password screens so the admin always sees *whose*
 * credential is being asked for.
 */
export default function AccountChip({ email, onClick }: AccountChipProps) {
  return (
    <IconTextActionCard
      variant="gradient"
      color="primary"
      icon={<UserRound />}
      title={email}
      onClick={onClick}
    />
  );
}
