/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { createHash } from "node:crypto";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  brandChecksum,
  buildPublishedBrand,
  imageRefs,
  parsePublishedBrand,
  readRainmakerBrand,
  serializeBrand,
  type RainmakerBrandError,
} from "./rainmaker-brand";
import { brandJson, CHAT_ICON_URL, FAN, LOGO, png, rainmaker, VALID, type ArchiveEntries } from "./rainmaker-brand.fixtures";

const LOGO_URL = "https://assets.example.com/eu-west-1/brand/images/aaaaaaaaaaaaaaaa.png";
const FAN_URL = "https://assets.example.com/eu-west-1/brand/images/bbbbbbbbbbbbbbbb.png";
const URLS = new Map([[LOGO, LOGO_URL], [FAN, FAN_URL]]);
const GARAGE_DOOR = "brand/images/garage-door.png";
const WELCOME = "brand/images/welcome.png";
const AVATAR = "brand/images/avatar.png";
const VIDEOS = { onboarding: { intro: "https://cdn.example.com/acme/intro.mp4" } };

/** A brand with every kind of key a later App Studio may add, including values the dashboard leaves to App Studio. */
const extendedBrand = brandJson(
  {
    ui: { radius: 40, fonts: { body: { family: "Inter" } }, colors: { light: { text_primary: "teal" } } },
    agents: { assistant: { avatar: AVATAR } },
    assets: {
      images: { devices: { fan: FAN, garageDoor: GARAGE_DOOR }, onboarding: { welcome: WELCOME } },
      videos: VIDEOS,
    },
  },
  { exportedBy: { tool: "App Studio", build: 42 } },
);

const extendedArchive: ArchiveEntries = {
  ...VALID,
  "brand/brand.json": extendedBrand,
  [GARAGE_DOOR]: png(16, 16),
  [WELCOME]: png(32, 32),
  [AVATAR]: png(8, 8),
};

const withBrand = (brand: object): ArchiveEntries => ({ ...VALID, "brand/brand.json": brand });
const { schemaVersion: _version, ...withoutVersion } = brandJson();
const { brandName: _name, ...configWithoutName } = brandJson().config;
const { supportedThemes: _themes, ...configWithoutThemes } = brandJson().config;

async function rejection(entries: ArchiveEntries) {
  return readRainmakerBrand(await rainmaker(entries)).then(
    () => null,
    (error: unknown) => error as RainmakerBrandError,
  );
}

afterEach(() => {
  vi.useRealTimers();
});

describe("readRainmakerBrand", () => {
  it("returns the brand and every archive image it refers to", async () => {
    const { brand, images } = await readRainmakerBrand(await rainmaker(VALID));

    expect(brand.config.brandName).toBe("Acme Home");
    expect([...images.keys()].sort()).toEqual([FAN, LOGO]);
  });

  it("keeps every key as written and finds archive images anywhere in the config", async () => {
    const { brand, images } = await readRainmakerBrand(await rainmaker(extendedArchive));

    expect(brand).toEqual(extendedBrand);
    expect([...images.keys()].sort()).toEqual([AVATAR, FAN, GARAGE_DOOR, LOGO, WELCOME]);
    expect(imageRefs(brand.config)).toEqual({
      "brandLogo.light": LOGO,
      "agents.assistant.avatar": AVATAR,
      "assets.images.devices.fan": FAN,
      "assets.images.devices.garageDoor": GARAGE_DOOR,
      "assets.images.onboarding.welcome": WELCOME,
    });
  });

  it.each([
    ["no brand.json", { ...VALID, "brand/brand.json": undefined }, "noBrand", /brand\/brand\.json/],
    ["a manifest from a newer format", { ...VALID, "manifest.json": { format: 2 } }, "notRainmaker", /format/],
    ["a missing schemaVersion", withBrand(withoutVersion), "invalidBrand", /^schemaVersion:/],
    ["a schemaVersion that is not a number", withBrand(brandJson({}, { schemaVersion: "1" })), "invalidBrand", /^schemaVersion:/],
    ["an unsupported schemaVersion", withBrand(brandJson({}, { schemaVersion: 2 })), "invalidBrand", /^schemaVersion: is not supported/],
    ["a missing supportedThemes", withBrand({ ...brandJson(), config: configWithoutThemes }), "invalidBrand", /^config\.supportedThemes:/],
    ["no supportedThemes", withBrand(brandJson({ supportedThemes: [] })), "invalidBrand", /^config\.supportedThemes:/],
    ["an unknown theme", withBrand(brandJson({ supportedThemes: ["sepia"] })), "invalidBrand", /^config\.supportedThemes\.0:/],
    ["a repeated theme", withBrand(brandJson({ supportedThemes: ["light", "light"] })), "invalidBrand", /^config\.supportedThemes: must not repeat/],
    ["a missing brandName", withBrand({ ...brandJson(), config: configWithoutName }), "invalidBrand", /^config\.brandName:/],
    ["an empty brandName", withBrand(brandJson({ brandName: "" })), "invalidBrand", /^config\.brandName:/],
    ["an SVG named .png", { ...VALID, [LOGO]: "<svg xmlns='http://www.w3.org/2000/svg'/>" }, "invalidImage", /logo\.png is not a PNG/],
    ["an image over 2048 px", { ...VALID, [LOGO]: png(2049, 1) }, "invalidImage", /2049x1/],
    ["a missing image", { ...VALID, [FAN]: undefined }, "invalidImage", /fan\.png is not in the file/],
    ["a path outside the archive", { ...VALID, "../evil.txt": "x" }, "notRainmaker", /unsafe/],
  ])("rejects %s", async (_case, entries, reason, detail) => {
    const error = await rejection(entries);

    expect(error?.reason).toBe(reason);
    expect(error?.detail).toMatch(detail);
  });

  it("rejects a file that is not a zip", async () => {
    const error = await readRainmakerBrand(new File(["not a zip"], "plain.rainmaker")).catch((e: unknown) => e);
    expect((error as RainmakerBrandError).reason).toBe("notRainmaker");
  });
});

describe("buildPublishedBrand", () => {
  it("publishes only schemaVersion, checksum, publishedAt and config, with archive paths swapped for URLs", async () => {
    vi.useFakeTimers({ now: Date.UTC(2026, 9, 28, 12, 0, 0, 750), toFake: ["Date"] });
    const { brand } = await readRainmakerBrand(await rainmaker(withBrand(brandJson({}, { exportedBy: "App Studio" }))));

    const published = await buildPublishedBrand(brand, URLS);

    expect(Object.keys(published)).toEqual(["schemaVersion", "checksum", "publishedAt", "config"]);
    expect(published.publishedAt).toBe(Date.UTC(2026, 9, 28, 12, 0, 0) / 1000);
    expect(published.config.brandLogo).toEqual({ light: LOGO_URL });
    expect(published.config.assets?.images).toEqual({ devices: { fan: FAN_URL }, agents: { chatIcon: CHAT_ICON_URL } });
    expect(published.config.supportedThemes).toEqual(["light", "dark"]);
    expect(published.checksum).toBe(createHash("sha256").update(JSON.stringify(published.config)).digest("hex"));
  });

  it("keeps every config key as written and rewrites archive images wherever they are", async () => {
    const { brand } = await readRainmakerBrand(await rainmaker(extendedArchive));
    const published = await buildPublishedBrand(brand, new Map([[WELCOME, LOGO_URL], [AVATAR, FAN_URL]]));

    expect(published.config.ui).toEqual(extendedBrand.config.ui);
    expect(published.config.agents).toEqual({ assistant: { avatar: FAN_URL } });
    expect(published.config.assets?.images?.onboarding).toEqual({ welcome: LOGO_URL });
    expect((published.config.assets as Record<string, unknown>).videos).toEqual(VIDEOS);
  });

  it("refuses a brand over 32 KB and accepts one over 16 KB", async () => {
    const palette = (count: number) => Object.fromEntries(Array.from({ length: count }, (_, i) => [`role${i}`, "#0369A1"]));
    const read = async (count: number) =>
      (await readRainmakerBrand(await rainmaker(withBrand(brandJson({ ui: { colors: { light: palette(count) } } }))))).brand;

    const mid = await buildPublishedBrand(await read(700), URLS);
    expect(new TextEncoder().encode(serializeBrand(mid)).length).toBeGreaterThan(16 * 1024);
    await expect(buildPublishedBrand(await read(1200), URLS)).rejects.toMatchObject({ reason: "tooLarge" });
  });
});

describe("brandChecksum", () => {
  it("ignores publishedAt and changes when an image does", async () => {
    const { brand } = await readRainmakerBrand(await rainmaker(VALID));
    vi.useFakeTimers({ now: Date.UTC(2026, 0, 1), toFake: ["Date"] });
    const first = await buildPublishedBrand(brand, URLS);
    vi.setSystemTime(Date.UTC(2026, 5, 1));
    const again = await buildPublishedBrand(brand, URLS);
    const changed = await buildPublishedBrand(brand, new Map([...URLS, [FAN, "https://assets.example.com/c.png"]]));

    expect(again.publishedAt).not.toBe(first.publishedAt);
    expect(again.checksum).toBe(first.checksum);
    expect(changed.checksum).not.toBe(first.checksum);
    expect(first.checksum).toBe(await brandChecksum(first.config));
  });
});

describe("parsePublishedBrand", () => {
  it("reads back what buildPublishedBrand writes, unknown keys included", async () => {
    const { brand } = await readRainmakerBrand(await rainmaker(extendedArchive));
    const published = await buildPublishedBrand(brand, URLS);

    expect(parsePublishedBrand(JSON.parse(serializeBrand(published)))).toEqual(published);
    expect(parsePublishedBrand({ ...published, mirror: "eu" })).toMatchObject({ mirror: "eu" });
  });

  it.each([
    ["an ISO publishedAt", { publishedAt: "2026-10-28T12:00:00Z" }, "publishedAt:"],
    ["a missing checksum", { checksum: undefined }, "checksum:"],
    ["an unsupported schemaVersion", { schemaVersion: 2 }, "schemaVersion: is not supported"],
  ])("rejects %s", async (_case, change, detail) => {
    const { brand } = await readRainmakerBrand(await rainmaker(VALID));
    const published = await buildPublishedBrand(brand, URLS);

    expect(() => parsePublishedBrand({ ...published, ...change })).toThrow(`invalidBrand: ${detail}`);
  });
});
