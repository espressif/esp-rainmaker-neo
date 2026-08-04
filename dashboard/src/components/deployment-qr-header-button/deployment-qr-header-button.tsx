/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { QrCode } from "lucide-react";
import { Button } from "@espressif/dashboard-ui-components/components";
import { useConfigStore } from "@/stores/config.store";
import { DeploymentDetailsQrDialog } from "@/components/deployment-details-qr-dialog";

/**
 * Header trigger for the deployment details QR dialog. Reads the deployment's
 * `SERVER_URL` from the runtime config store and renders nothing until it is
 * available (e.g. a stale persisted config from before SERVER_URL was stored).
 */
export default function DeploymentQrHeaderButton() {
  const { t } = useTranslation("common");
  const serverUrl = useConfigStore((s) => s.config?.SERVER_URL);
  const [isDialogOpen, setIsDialogOpen] = useState(false);

  if (!serverUrl) {
    return null;
  }

  const buttonLabel = t("deploymentQr.showButton", "Show deployment QR code");

  return (
    <>
      <Button
        type="button"
        variant="ghost"
        color="secondary"
        size="icon"
        className="h-6 w-8"
        startIcon={<QrCode />}
        onClick={() => setIsDialogOpen(true)}
        aria-label={buttonLabel}
        tooltip={buttonLabel}
      />
      <DeploymentDetailsQrDialog
        url={serverUrl}
        open={isDialogOpen}
        onOpenChange={setIsDialogOpen}
      />
    </>
  );
}
