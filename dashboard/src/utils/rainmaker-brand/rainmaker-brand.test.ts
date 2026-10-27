/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from "vitest";
import {
  brandChecksum,
  buildPublishedBrand,
  parsePublishedBrand,
  readRainmakerBrand,
  type RainmakerBrandError,
} from "./rainmaker-brand";
import { brandJson, FAN, LOGO, png, rainmaker, VALID, type ArchiveEntries } from "./rainmaker-brand.fixtures";

async function rejection(entries: ArchiveEntries) {
  return readRainmakerBrand(await rainmaker(entries)).then(
    () => null,
    (error: unknown) => error as RainmakerBrandError,
  );
}

describe("readRainmakerBrand", () => {
  it("returns the brand and every image it refers to", async () => {
    const { brand, images } = await readRainmakerBrand(await rainmaker(VALID));

    expect(brand.config.appName).toBe("Acme Home");
    expect([...images.keys()].sort()).toEqual([FAN, LOGO]);
  });

  it.each([
    ["no brand folder", { ...VALID, "brand/brand.json": undefined }, "noBrand"],
    ["a manifest from a newer format", { ...VALID, "manifest.json": { format: 2 } }, "notRainmaker"],
    ["an unknown config field", { ...VALID, "brand/brand.json": brandJson({ fontFamily: "Serif" }) }, "invalidBrand"],
    ["a bad colour", { ...VALID, "brand/brand.json": brandJson({ colors: { light: { primary: "#12345" } } }) }, "invalidBrand"],
    ["schema 2", { ...VALID, "brand/brand.json": { ...brandJson(), schema: 2 } }, "invalidBrand"],
    ["an SVG named .png", { ...VALID, [LOGO]: "<svg xmlns='http://www.w3.org/2000/svg'/>" }, "invalidImage"],
    ["an image over 2048 px", { ...VALID, [LOGO]: png(2049, 1) }, "invalidImage"],
    ["a missing image", { ...VALID, [FAN]: undefined }, "invalidImage"],
    ["a path outside the archive", { ...VALID, "../evil.txt": "x" }, "notRainmaker"],
  ])("rejects %s", async (_case, entries, reason) => {
    expect((await rejection(entries))?.reason).toBe(reason);
  });

  it("rejects a file that is not a zip", async () => {
    const error = await readRainmakerBrand(new File(["not a zip"], "plain.rainmaker")).catch((e: unknown) => e);
    expect((error as RainmakerBrandError).reason).toBe("notRainmaker");
  });
});

describe("buildPublishedBrand", () => {
  it("replaces archive paths with their public URLs and keeps a hidden wordmark", async () => {
    const { brand } = await readRainmakerBrand(await rainmaker(VALID));
    const urls = new Map([
      [LOGO, "https://assets.example.com/eu-west-1/brand/images/aaaa.png"],
      [FAN, "https://assets.example.com/eu-west-1/brand/images/bbbb.png"],
    ]);

    const published = await buildPublishedBrand(brand, urls);

    expect(published.config.logoUrl).toBe(urls.get(LOGO));
    expect(published.config.assets).toEqual({ fan: urls.get(FAN) });
    expect(published.config.wordmark).toBe("hidden");
    expect(published.checksum).toBe(await brandChecksum("both", published.config));
  });

  it("refuses a brand over 16 KB", async () => {
    const roles = Object.fromEntries(Array.from({ length: 600 }, (_, i) => [`role${i}`, "#0369A1"]));
    const { brand } = await readRainmakerBrand(
      await rainmaker({ ...VALID, "brand/brand.json": brandJson({ colors: { light: roles } }) }),
    );
    await expect(buildPublishedBrand(brand, new Map())).rejects.toMatchObject({ reason: "tooLarge" });
  });
});

describe("parsePublishedBrand", () => {
  it("accepts what buildPublishedBrand writes and rejects an unknown field", async () => {
    const { brand } = await readRainmakerBrand(await rainmaker(VALID));
    const published = await buildPublishedBrand(brand, new Map([[LOGO, "https://assets.example.com/a.png"], [FAN, "https://assets.example.com/b.png"]]));

    expect(parsePublishedBrand(JSON.parse(JSON.stringify(published)))).toEqual(published);
    expect(() => parsePublishedBrand({ ...published, extra: true })).toThrow(/invalidBrand/);
  });
});

describe("brandChecksum", () => {
  it("stays the same when the same brand is published again and changes when an image does", async () => {
    const { brand } = await readRainmakerBrand(await rainmaker(VALID));
    const urls = (fan: string) => new Map([[LOGO, "https://assets.example.com/a.png"], [FAN, fan]]);

    const first = await buildPublishedBrand(brand, urls("https://assets.example.com/b.png"));
    const again = await buildPublishedBrand(brand, urls("https://assets.example.com/b.png"));
    const changed = await buildPublishedBrand(brand, urls("https://assets.example.com/c.png"));

    expect(again.checksum).toBe(first.checksum);
    expect(changed.checksum).not.toBe(first.checksum);
  });
});
