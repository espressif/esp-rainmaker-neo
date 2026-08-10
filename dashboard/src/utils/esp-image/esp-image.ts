/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

// Parses the fixed-layout header of an ESP-IDF application image to recover the
// metadata the firmware carries about itself (esp_app_desc_t): version, project
// name (the RainMaker "model") and target chip. Reference: ESP-IDF "App Image
// Format" — esp_image_header_t at offset 0, then an 8-byte segment header, then
// esp_app_desc_t at the documented fixed offset 0x20. Reading esp_app_desc_t
// sequentially after the 24-byte image header (as legacy RainMaker did) lands 8
// bytes early, which is why this parser uses absolute offsets and validates the
// app-descriptor magic word.

const IMAGE_MAGIC = 0xe9;
const APP_DESC_MAGIC_WORD = 0xabcd5432;

const CHIP_ID_OFFSET = 12;
const APP_DESC_OFFSET = 0x20;
const APP_DESC_SIZE = 256;
const SECURE_VERSION_OFFSET = APP_DESC_OFFSET + 4;
const VERSION_OFFSET = APP_DESC_OFFSET + 16;
const PROJECT_NAME_OFFSET = APP_DESC_OFFSET + 48;
const IDF_VER_OFFSET = APP_DESC_OFFSET + 112;
const STRING_FIELD_SIZE = 32;

/** Minimum bytes needed to parse: everything up to the end of esp_app_desc_t. */
export const ESP_IMAGE_PARSE_LENGTH = APP_DESC_OFFSET + APP_DESC_SIZE;

/** esp_chip_id_t values from ESP-IDF's esp_app_format.h. */
const CHIP_ID_TO_PLATFORM: Record<number, string> = {
  0x0000: "esp32",
  0x0002: "esp32s2",
  0x0005: "esp32c3",
  0x0009: "esp32s3",
  0x000c: "esp32c2",
  0x000d: "esp32c6",
  0x0010: "esp32h2",
  0x0012: "esp32p4",
};

export interface EspImageInfo {
  chipId: number;
  platform?: string;
  fwVersion?: string;
  model?: string;
  idfVersion?: string;
  secureVersion: number;
}

export type ParseEspImageResult =
  | { ok: true; info: EspImageInfo }
  | { ok: false; reason: "too-short" | "bad-image-magic" | "bad-app-desc-magic" };

function readNulTerminatedString(
  view: DataView,
  offset: number,
  fieldSize: number,
): string | undefined {
  const bytes = new Uint8Array(view.buffer, view.byteOffset + offset, fieldSize);
  const nul = bytes.indexOf(0);
  const value = new TextDecoder()
    .decode(nul === -1 ? bytes : bytes.subarray(0, nul))
    .trim();
  return value === "" ? undefined : value;
}

export function parseEspAppImage(buffer: ArrayBuffer): ParseEspImageResult {
  if (buffer.byteLength < ESP_IMAGE_PARSE_LENGTH) {
    return { ok: false, reason: "too-short" };
  }

  const view = new DataView(buffer);
  if (view.getUint8(0) !== IMAGE_MAGIC) {
    return { ok: false, reason: "bad-image-magic" };
  }
  if (view.getUint32(APP_DESC_OFFSET, true) !== APP_DESC_MAGIC_WORD) {
    return { ok: false, reason: "bad-app-desc-magic" };
  }

  const chipId = view.getUint16(CHIP_ID_OFFSET, true);
  return {
    ok: true,
    info: {
      chipId,
      platform: CHIP_ID_TO_PLATFORM[chipId],
      fwVersion: readNulTerminatedString(view, VERSION_OFFSET, STRING_FIELD_SIZE),
      model: readNulTerminatedString(view, PROJECT_NAME_OFFSET, STRING_FIELD_SIZE),
      idfVersion: readNulTerminatedString(view, IDF_VER_OFFSET, STRING_FIELD_SIZE),
      secureVersion: view.getUint32(SECURE_VERSION_OFFSET, true),
    },
  };
}
