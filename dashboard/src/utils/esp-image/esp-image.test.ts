/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from "vitest";
import { parseEspAppImage } from "./esp-image";

// Byte layout of an ESP-IDF app image (all values little-endian, packed):
//   0x00  esp_image_header_t   (24 B)  magic 0xE9 at 0, chip_id u16 at 12
//   0x18  segment header       (8 B)   sits between the two structs we read
//   0x20  esp_app_desc_t       (256 B) magic_word 0xABCD5432, secure_version,
//         reserv1[8], version[32] @48, project_name[32] @80, time[16], date[16],
//         idf_ver[32] @144, app_elf_sha256[32], reserved
const APP_DESC_END = 0x20 + 256;

function writeString(
  buf: ArrayBuffer,
  offset: number,
  value: string,
  fieldSize: number,
): void {
  const bytes = new TextEncoder().encode(value).slice(0, fieldSize);
  new Uint8Array(buf, offset, fieldSize).fill(0);
  new Uint8Array(buf, offset, bytes.length).set(bytes);
}

function buildEspImage(overrides?: {
  imageMagic?: number;
  chipId?: number;
  appDescMagic?: number;
  secureVersion?: number;
  version?: string;
  projectName?: string;
  idfVersion?: string;
  length?: number;
}): ArrayBuffer {
  const {
    imageMagic = 0xe9,
    chipId = 0x0000,
    appDescMagic = 0xabcd5432,
    secureVersion = 0,
    version = "1.2.3",
    projectName = "thermostat_fw",
    idfVersion = "v5.2.1",
    length = 4096,
  } = overrides ?? {};

  const buf = new ArrayBuffer(length);
  const view = new DataView(buf);
  view.setUint8(0, imageMagic);
  view.setUint16(12, chipId, true);
  view.setUint32(0x20, appDescMagic, true);
  view.setUint32(0x24, secureVersion, true);
  writeString(buf, 48, version, 32);
  writeString(buf, 80, projectName, 32);
  writeString(buf, 144, idfVersion, 32);
  return buf;
}

describe("parseEspAppImage", () => {
  it("extracts fw version and model (project name) from a valid image", () => {
    const result = parseEspAppImage(
      buildEspImage({ version: "2.0.1", projectName: "led_strip" }),
    );

    expect(result).toMatchObject({
      ok: true,
      info: { fwVersion: "2.0.1", model: "led_strip", idfVersion: "v5.2.1" },
    });
  });

  it("maps chip_id to a platform string", () => {
    const result = parseEspAppImage(buildEspImage({ chipId: 0x0005 }));

    expect(result).toMatchObject({
      ok: true,
      info: { chipId: 0x0005, platform: "esp32c3" },
    });
  });

  it("returns the raw chip_id with no platform for an unknown chip", () => {
    const result = parseEspAppImage(buildEspImage({ chipId: 0x7fff }));

    expect(result).toMatchObject({ ok: true, info: { chipId: 0x7fff } });
    if (result.ok) {
      expect(result.info.platform).toBeUndefined();
    }
  });

  it("reports secure_version", () => {
    const result = parseEspAppImage(buildEspImage({ secureVersion: 7 }));

    expect(result).toMatchObject({ ok: true, info: { secureVersion: 7 } });
  });

  it("rejects a buffer shorter than the app descriptor's end", () => {
    const result = parseEspAppImage(
      buildEspImage({ length: APP_DESC_END - 1 }).slice(0, APP_DESC_END - 1),
    );

    expect(result).toEqual({ ok: false, reason: "too-short" });
  });

  it("rejects a file that does not start with the 0xE9 image magic", () => {
    const result = parseEspAppImage(buildEspImage({ imageMagic: 0x7f }));

    expect(result).toEqual({ ok: false, reason: "bad-image-magic" });
  });

  it("rejects an image whose app descriptor magic word is wrong", () => {
    const result = parseEspAppImage(buildEspImage({ appDescMagic: 0xdeadbeef }));

    expect(result).toEqual({ ok: false, reason: "bad-app-desc-magic" });
  });

  it("trims strings at the first NUL and surrounding whitespace", () => {
    const result = parseEspAppImage(buildEspImage({ version: " 1.0.0 " }));

    expect(result).toMatchObject({ ok: true, info: { fwVersion: "1.0.0" } });
  });

  it("handles a string that fills its 32-byte field with no NUL terminator", () => {
    const full = "a".repeat(32);
    const result = parseEspAppImage(buildEspImage({ projectName: full }));

    expect(result).toMatchObject({ ok: true, info: { model: full } });
  });

  it("omits empty string fields instead of returning empty strings", () => {
    const result = parseEspAppImage(buildEspImage({ version: "" }));

    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.info.fwVersion).toBeUndefined();
    }
  });
});
