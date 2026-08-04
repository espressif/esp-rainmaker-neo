/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  Dialog,
  DialogContent,
} from "@espressif/dashboard-ui-components/components";
import { DeploymentDetailsQr } from "@/components/deployment-details-qr";
import type { DeploymentDetailsQrDialogProps } from "./deployment-details-qr-dialog.props";

/**
 * Dialog wrapper around {@link DeploymentDetailsQr}. The QR card is mounted
 * only while open so the code regenerates on every open.
 */
export default function DeploymentDetailsQrDialog({
  url,
  open,
  onOpenChange,
}: DeploymentDetailsQrDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="p-0 rounded-3xl" showCloseButton={false}>
        {open ? <DeploymentDetailsQr url={url} /> : null}
      </DialogContent>
    </Dialog>
  );
}
