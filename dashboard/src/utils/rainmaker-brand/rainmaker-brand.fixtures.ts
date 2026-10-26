/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import JSZip from "jszip";

export const LOGO = "brand/images/logo.png";
export const FAN = "brand/images/asset-fan.png";

/** Smallest byte layout readRainmakerBrand checks: the PNG signature, then an IHDR chunk carrying width and height. */
export function png(width = 4, height = 4): Uint8Array {
  const bytes = new Uint8Array(33);
  bytes.set([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
  bytes.set(new TextEncoder().encode("IHDR"), 12);
  const view = new DataView(bytes.buffer);
  view.setUint32(16, width);
  view.setUint32(20, height);
  return bytes;
}

export function brandJson(config: Record<string, unknown> = {}) {
  return {
    schema: 1,
    generatedBy: "ESP RainMaker Neo App Studio",
    appearance: "both",
    config: {
      whitelabelSchema: 1,
      mode: "light",
      appName: "Acme Home",
      logoUrl: LOGO,
      wordmark: "hidden",
      assets: { fan: FAN },
      colors: { light: { primary: "#0369A1", lightBlue: "rgba(3, 105, 161, 0.3)" } },
      ...config,
    },
  };
}

export type ArchiveEntries = Record<string, string | Uint8Array | object | undefined>;

/** Zips the entries into a .rainmaker file; an `undefined` entry is left out. */
export async function rainmaker(entries: ArchiveEntries): Promise<File> {
  const zip = new JSZip();
  for (const [name, content] of Object.entries(entries)) {
    if (content === undefined) {
      continue;
    }
    zip.file(name, content instanceof Uint8Array || typeof content === "string" ? content : JSON.stringify(content));
  }
  return new File([await zip.generateAsync({ type: "arraybuffer" })], "acme.rainmaker");
}

export const VALID = {
  "manifest.json": { format: 1 },
  "project.yaml": "never: read",
  "brand/brand.json": brandJson(),
  [LOGO]: png(),
  [FAN]: png(),
};
