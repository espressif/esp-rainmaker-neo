/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

// Derives VITE_SERVER_URL for `npm run dev`, mirroring scripts/upload_rmng_outputs.py: the
// client-outputs file always lives in the rmng-public-assets-<ACCOUNT_ID> bucket in us-east-1,
// partitioned by <REGION>/.
// Writes .env.development.local (gitignored, higher precedence than .env.development) so nothing
// account-specific is ever committed. No-op when VITE_SERVER_URL is already set in the shell.
//
// Account + region come from rmng-outputs.json (repo root) when present — StackAccountId /
// StackRegion under rmng-base, written by scripts/generate_stack_outputs.py from the actual
// deployment. That's what was really deployed, so it can't drift from a stray AWS_PROFILE/
// AWS_REGION left set in the shell (see git history: this bit devs more than once). Falls back
// to `aws sts`/`aws configure` — the shell's current identity — only when that file is absent.
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const BUCKET_REGION = "us-east-1"; // rmng-public-assets bucket is always us-east-1
const REPO_ROOT = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const OUTPUTS_FILE = join(REPO_ROOT, "rmng-outputs.json");

function sh(cmd, args) {
  return execFileSync(cmd, args, { encoding: "utf8" }).trim();
}

function fromOutputsFile() {
  if (!existsSync(OUTPUTS_FILE)) {
    return null;
  }
  const base = JSON.parse(readFileSync(OUTPUTS_FILE, "utf8"))["rmng-base"];
  const account = base?.StackAccountId;
  const region = base?.StackRegion;
  if (!account || !region) {
    return null;
  }
  console.log(`[derive-server-url] using account/region from ${OUTPUTS_FILE}`);
  return { account, region };
}

function fromShellIdentity() {
  const account = sh("aws", [
    "sts",
    "get-caller-identity",
    "--query",
    "Account",
    "--output",
    "text",
  ]);
  const region =
    process.env.AWS_REGION || sh("aws", ["configure", "get", "region"]);
  if (!account || !region) {
    throw new Error("empty account or region");
  }
  console.log(
    "[derive-server-url] rmng-outputs.json not found; using current AWS CLI identity",
  );
  return { account, region };
}

if (process.env.VITE_SERVER_URL) {
  process.exit(0); // explicit shell value wins; don't override it
}

try {
  const { account, region } = fromOutputsFile() ?? fromShellIdentity();

  const url = `https://rmng-public-assets-${account}.s3.${BUCKET_REGION}.amazonaws.com/${region}/rmng-client-outputs.json`;
  const target = join(
    dirname(fileURLToPath(import.meta.url)),
    "..",
    ".env.development.local",
  );
  writeFileSync(target, `VITE_SERVER_URL=${url}\n`);
  console.log(`[derive-server-url] VITE_SERVER_URL -> ${url}`);
} catch (err) {
  // Non-fatal: a dev without AWS creds can still set VITE_SERVER_URL by hand.
  console.warn(
    `[derive-server-url] could not derive VITE_SERVER_URL (${err.message}).`,
  );
  console.warn(
    "[derive-server-url] set it manually in .env.development.local or export it before `npm run dev`.",
  );
}
