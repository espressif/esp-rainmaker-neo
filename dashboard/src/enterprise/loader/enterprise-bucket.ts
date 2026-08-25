/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * The Espressif-owned central bucket all deployments load enterprise features from. Bucket
 * identity is public information and therefore an embedded constant, like the signing public
 * key: entitlement is enforced by the bucket policy in Espressif's account plus the read grant
 * the enterprise CDK addon attaches to the deployment's browser role — never by this file.
 * Deployments without the grant get AccessDenied and silently stay in community mode.
 *
 * Prototype values: the stand-in Espressif account is `Insights Test 1` (311057549898).
 */

export const ENTERPRISE_ASSETS_BUCKET = 'rmng-enterprise-assets-311057549898'

/** The central bucket's own region — S3 clients must target it, not the deployment's region. */
export const ENTERPRISE_ASSETS_REGION = 'us-east-1'

/** One directory per feature under this prefix; discovered by listing (delimiter `/`). */
export const ENTERPRISE_FEATURES_PREFIX = 'admin-dashboard/features/'
