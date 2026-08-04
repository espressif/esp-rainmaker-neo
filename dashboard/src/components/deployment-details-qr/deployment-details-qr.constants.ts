/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { QRCodeToDataURLOptions } from "qrcode";

export const DEPLOYMENT_QR_OPTIONS: QRCodeToDataURLOptions = {
  type: "image/png",
  width: 300,
  margin: 2,
};

export const DEPLOYMENT_QR_DOWNLOAD_FILENAME = "deployment-details-qr.png";

export const DEPLOYMENT_QR_SPINNER_SIZE = 24;
