/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from "vitest";
import type {
  BrandConfig,
  PublishedBrand,
} from "@/utils/rainmaker-brand/rainmaker-brand";
import { getBrandPreview } from "./branding.utils";

const LIGHT_LOGO = "https://cdn.example.com/light.png";
const DARK_LOGO = "https://cdn.example.com/dark.png";

function published(
  config: Partial<BrandConfig> & Record<string, unknown>,
  themes: BrandConfig["supportedThemes"] = ["light", "dark"],
): PublishedBrand {
  return {
    schemaVersion: 1,
    checksum: "0".repeat(64),
    publishedAt: 0,
    config: { brandName: "Acme Home", supportedThemes: themes, ...config },
  };
}

describe("getBrandPreview", () => {
  it("shows each theme its own logo, else the other theme's, never an unknown key's", () => {
    const preview = getBrandPreview(
      published({
        brandLogo: {
          highContrast: "https://cdn.example.com/hc.png",
          dark: DARK_LOGO,
        } as BrandConfig["brandLogo"],
      }),
    );
    expect(preview.logos.map(({ mode, src }) => [mode, src])).toEqual([
      ["light", DARK_LOGO],
      ["dark", DARK_LOGO],
    ]);
  });

  it("uses the other theme's logo when a theme's is empty", () => {
    const preview = getBrandPreview(
      published({ brandLogo: { light: LIGHT_LOGO, dark: "" } }),
    );
    expect(preview.logos.map(({ src }) => src)).toEqual([
      LIGHT_LOGO,
      LIGHT_LOGO,
    ]);
  });

  it("skips colours and words that are not text, which publishing does not check", () => {
    const preview = getBrandPreview(
      published({
        ui: {
          colors: { light: { primary: "#0369A1", bg: { value: "#FFFFFF" } } },
        } as unknown as BrandConfig["ui"],
        glossary: {
          device: "Gadget",
          room: { en: "Space" },
        } as unknown as BrandConfig["glossary"],
      }),
    );
    expect(preview.palettes).toEqual([
      {
        mode: "light",
        swatches: [
          {
            role: "primary",
            value: "#0369A1",
            style: { backgroundColor: "#0369A1" },
          },
        ],
      },
    ]);
    expect(preview.words).toEqual([["device", "Gadget"]]);
  });

  it("shows palettes only for the themes the brand supports", () => {
    const preview = getBrandPreview(
      published(
        { ui: { colors: { light: { primary: "#0369A1" }, dark: { primary: "#38BDF8" } } } },
        ["dark"],
      ),
    );
    expect(preview.palettes.map(({ mode }) => mode)).toEqual(["dark"]);
  });
});
