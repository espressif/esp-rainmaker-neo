/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { CreateMenu } from "@/components/create-menu";
import { DeploymentQrHeaderButton } from "@/components/deployment-qr-header-button";

/**
 * Right-side content of the app header. New global header actions belong here,
 * not in `home.tsx` or inside the individual action components.
 */
export default function HeaderRightContent() {
  return (
    <div className="flex items-center justify-end gap-2">
      <DeploymentQrHeaderButton />
      <CreateMenu />
    </div>
  );
}
