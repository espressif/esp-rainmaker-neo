/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Base64 (standard) DER/SPKI encoding of the manifest-signing public key (ECDSA P-256). Public
 * keys are safe to publish; the private half never leaves KMS in Espressif's account. Populated
 * from `node scripts/export-public-key.mjs` in the plugins repo (or `aws kms get-public-key
 * --key-id alias/rmng-ent-manifest-signing`) after provisioning. While empty, production builds
 * treat every deployment as community — nothing loads.
 */
export const MANIFEST_PUBLIC_KEY_SPKI_B64 =
  'MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEG9RgOBbEluCngJxVgAmi54o8170B3U6nxLOqmG3WEq93SyTz4vBTCeFc+s/YfuYDjknMd2QjsU48Ge1xmVD55w=='
