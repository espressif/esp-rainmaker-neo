/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from "vitest";
import type { EspImageInfo } from "@/utils/esp-image/esp-image";
import {
  computeOtaImagePrefill,
  lockedFieldsFor,
  NO_LOCKED_OTA_IMAGE_FIELDS,
} from "./ota-image-prefill";

function imageInfo(overrides?: Partial<EspImageInfo>): EspImageInfo {
  return {
    chipId: 0x0005,
    platform: "esp32c3",
    fwVersion: "1.4.0",
    model: "smart_plug",
    idfVersion: "v5.2.1",
    secureVersion: 0,
    ...overrides,
  };
}

describe("computeOtaImagePrefill", () => {
  it("takes version, model and platform from the image", () => {
    expect(computeOtaImagePrefill(imageInfo())).toEqual({
      version: "1.4.0",
      model: "smart_plug",
      platform: "esp32c3",
    });
  });

  it("skips fields the image does not provide", () => {
    const result = computeOtaImagePrefill(
      imageInfo({ fwVersion: undefined, platform: undefined }),
    );

    expect(result).toEqual({ model: "smart_plug" });
  });

  it("returns nothing for an image that carries no usable metadata", () => {
    const result = computeOtaImagePrefill(
      imageInfo({ fwVersion: undefined, model: undefined, platform: undefined }),
    );

    expect(result).toEqual({});
  });
});

describe("lockedFieldsFor", () => {
  it("locks every field the image filled", () => {
    const locked = lockedFieldsFor(computeOtaImagePrefill(imageInfo()));

    expect([...locked].sort()).toEqual(["model", "platform", "version"]);
  });

  it("leaves fields the image did not fill unlocked", () => {
    const locked = lockedFieldsFor(
      computeOtaImagePrefill(imageInfo({ platform: undefined })),
    );

    expect(locked.has("platform")).toBe(false);
    expect(locked.has("version")).toBe(true);
  });

  it("reuses the shared empty set so an empty extraction does not re-render", () => {
    expect(lockedFieldsFor({})).toBe(NO_LOCKED_OTA_IMAGE_FIELDS);
  });
});
