/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { FirmwareFileRow } from "@/aws/services/firmware-upload.service";

/**
 * Row model for the OTA Images table. Identical in shape to the S3 service's
 * {@link FirmwareFileRow}: `key/name/size/md5/lastModified` arrive from the
 * List call, while `version/type/model/platform` are filled in progressively
 * from the per-object tagging call.
 */
export type OtaImageRow = FirmwareFileRow;
