/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { beforeEach, describe, expect, it, vi } from "vitest";
import { deleteBrand, publishBrand, readPublishedBrand } from "./brand.service";
import { BRAND_KEY, FakeBucket, IMAGES_PREFIX, ORIGIN, OUTPUTS_URL } from "./brand.service.fixtures";
import { brandJson, CHAT_ICON_URL, FAN, png, rainmaker, VALID } from "@/utils/rainmaker-brand/rainmaker-brand.fixtures";

const HOSTED_IMAGE = new RegExp(`^${ORIGIN}/${IMAGES_PREFIX}[0-9a-f]{16}\\.png$`);
const WELCOME = "brand/images/welcome.png";
const bucket = new FakeBucket();

vi.mock("./s3-client", () => ({ getS3Client: () => bucket }));
vi.mock("@/lib/config", () => ({ getServerUrl: () => OUTPUTS_URL }));

const keyOf = (url: string | undefined) => url?.slice(ORIGIN.length + 1);
const OLD_LOGO_KEY = `${IMAGES_PREFIX}0123456789abcdef.png`;

/** A brand.json in the format this dashboard published before schemaVersion 1, with one hosted image. */
function storeOldFormatBrand() {
  bucket.objects.set(OLD_LOGO_KEY, { body: png() });
  bucket.objects.set(BRAND_KEY, {
    body: JSON.stringify({ schema: 1, appearance: "light", config: { appName: "Old", logoUrl: `${ORIGIN}/${OLD_LOGO_KEY}` } }),
  });
}

beforeEach(() => {
  bucket.reset();
  vi.stubGlobal("fetch", bucket.fetch);
});

describe("publishBrand", () => {
  it("uploads every image before brand.json, which phones always revalidate", async () => {
    const brand = await publishBrand(await rainmaker(VALID));

    expect(bucket.writes.at(-1)).toBe(`put ${BRAND_KEY}`);
    expect(bucket.objects.get(BRAND_KEY)?.cacheControl).toBe("no-cache");
    expect(bucket.images()).toHaveLength(2);
    expect(brand.config.brandLogo?.light).toMatch(HOSTED_IMAGE);
    expect(brand.config.assets?.images?.agents).toEqual({ chatIcon: CHAT_ICON_URL });
    await expect(readPublishedBrand()).resolves.toEqual(brand);
  });

  it("uploads and rewrites an image in a group and under a name it has never seen", async () => {
    const brand = await publishBrand(
      await rainmaker({
        ...VALID,
        "brand/brand.json": brandJson({ assets: { images: { onboarding: { welcomeHero: WELCOME } } } }),
        [WELCOME]: png(16, 16),
      }),
    );

    const welcome = brand.config.assets?.images?.onboarding?.welcomeHero;
    expect(welcome).toMatch(HOSTED_IMAGE);
    expect(bucket.images()).toContain(keyOf(welcome));
  });

  it("removes only the previous brand's images the new one does not use", async () => {
    const first = await publishBrand(await rainmaker(VALID));
    const replaced = await publishBrand(
      await rainmaker({
        ...VALID,
        "brand/brand.json": brandJson({ assets: { images: { devices: { fan: "https://cdn.example.com/fan.png" } } } }),
      }),
    );

    expect(replaced.config.brandLogo).toEqual(first.config.brandLogo);
    expect(bucket.images()).toEqual([keyOf(first.config.brandLogo?.light)]);
    expect(replaced.config.assets?.images?.devices).toEqual({ fan: "https://cdn.example.com/fan.png" });
  });

  it("removes the images of a previous brand.json it cannot read", async () => {
    storeOldFormatBrand();
    await expect(readPublishedBrand()).rejects.toMatchObject({ reason: "invalidBrand" });

    const brand = await publishBrand(await rainmaker(VALID));

    expect(bucket.images()).not.toContain(OLD_LOGO_KEY);
    expect(bucket.images()).toContain(keyOf(brand.config.brandLogo?.light));
  });

  it("writes nothing when the file is rejected", async () => {
    await expect(publishBrand(await rainmaker({ ...VALID, [FAN]: undefined }))).rejects.toMatchObject({
      reason: "invalidImage",
    });
    expect(bucket.writes).toEqual([]);
  });
});

describe("readPublishedBrand", () => {
  it("is null when the deployment has no brand", async () => {
    await expect(readPublishedBrand()).resolves.toBeNull();
  });

  it("throws when the bucket fails", async () => {
    bucket.readStatus = 500;

    await expect(readPublishedBrand()).rejects.toThrow("HTTP 500");
  });
});

describe("deleteBrand", () => {
  it("removes brand.json, then its images", async () => {
    await publishBrand(await rainmaker(VALID));
    bucket.writes = [];

    await deleteBrand();

    expect(bucket.writes[0]).toBe(`delete ${BRAND_KEY}`);
    expect(bucket.objects.size).toBe(0);
    await expect(readPublishedBrand()).resolves.toBeNull();
  });

  it("removes a brand.json it cannot read, and its images", async () => {
    storeOldFormatBrand();

    await deleteBrand();

    expect(bucket.objects.size).toBe(0);
  });
});
