/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { RuntimeConfig } from '@/lib/config'

/**
 * Runtime config is fetched from a published `rmng-client-outputs.json` document.
 *
 * The dev build points at it directly via `VITE_SERVER_URL`. The deployed build
 * reads its URL from `/config.json` (`{ "SERVER_URL": ... }`) written by the
 * dashboard CDK stack. Both then fetch the stack-keyed client-outputs document,
 * which `mapClientOutputs` flattens into `RuntimeConfig`.
 */

/** Shape of the deployed `/config.json` pointer file. */
interface ConfigPointer {
  SERVER_URL?: string
}

/**
 * The published client-outputs document is keyed by CDK stack name. Most stacks publish flat
 * string outputs; the per-region Alexa stacks nest theirs under a `regions` map, so a value is
 * not necessarily a string.
 */
type ClientOutputs = Record<string, Record<string, unknown> | undefined>

/** Flat string outputs, which is what every stack but the Alexa ones publishes. */
type StringOutputs = Record<string, string | undefined>

/**
 * A Smart Home skill is given one endpoint per Alexa region, so the deployment publishes an ARN
 * for each. They live under the Alexa stack for this deployment's region, keyed by the Alexa
 * region the endpoint serves.
 */
function alexaSkillArns(outputs: ClientOutputs, region: string | undefined): Record<string, string> {
  const regions = region ? outputs[`rmng-alexa-core-${region}`]?.regions : undefined
  if (!regions || typeof regions !== 'object') {
    return {}
  }
  const arns: Record<string, string> = {}
  for (const [alexaRegion, values] of Object.entries(regions as Record<string, unknown>)) {
    const arn = (values as StringOutputs | undefined)?.AlexaSkillFunctionArn
    if (typeof arn === 'string' && arn) {
      arns[alexaRegion] = arn
    }
  }
  return arns
}

/**
 * Flatten the stack-keyed client-outputs JSON (the same file `morpheus.py
 * --client-outputs` reads) into the flat `RuntimeConfig` the app consumes.
 * Only `rmng-base`, `rmng-core` and `espuser-base` are consumed today.
 */
function mapClientOutputs(outputs: ClientOutputs): RuntimeConfig {
  const base = (outputs['rmng-base'] ?? {}) as StringOutputs
  const gva = (outputs['rmng-gva-core'] ?? {}) as StringOutputs
  const espUser = (outputs['espuser-base'] ?? {}) as StringOutputs
  return {
    API_GATEWAY_URL: base.ApiGatewayUrl,
    ESP_USER_API_URL: espUser.EspUserApiUrl,
    COGNITO_USER_POOL_ID: base.AdminUserPoolId,
    COGNITO_CLIENT_ID: base.AdminUserPoolClientId,
    REGION: base.StackRegion,
    IOT_ENDPOINT: base.IoTEndpointUrl,
    OTA_S3_BUCKET: base.FilesBucketName,
    OTA_SERVICE_ROLE_ARN: base.OtaServiceRoleArn,
    CREDENTIAL_PROVIDER_ENDPOINT: base.CredentialProviderEndpoint,
    FILES_BUCKET_NAME: base.FilesBucketName,
    AUTHORIZE_URL: espUser.EspUserAuthorizeUrl,
    TOKEN_URL: espUser.EspUserTokenUrl,
    OIDC_CLIENT_ID: espUser.EspAdminUserPoolClientId,
    VA_CLIENT_ID: espUser.EspVaClientId,
    GVA_FULFILLMENT_URL: gva.GVAFulfillmentUrl,
    GVA_ENABLED: Boolean(outputs['rmng-gva-core']),
    ALEXA_SKILL_ARNS: alexaSkillArns(outputs, base.StackRegion),
    ALEXA_ENABLED: Boolean(outputs['rmng-alexa-cfg-core']),
    BRIDGE_ENABLED: Boolean(outputs['rmng-bridge-core']),
  }
}

/**
 * Resolve the URL of the client-outputs document.
 * Dev: `VITE_SERVER_URL` (derived by scripts/derive-server-url.mjs).
 * Prod: `GET /config.json` -> `SERVER_URL`.
 * Throws when no URL can be determined so the caller can surface an error.
 */
async function resolveClientOutputsUrl(): Promise<string> {
  const devUrl = import.meta.env.VITE_SERVER_URL
  if (devUrl) {
    return devUrl
  }
  if (import.meta.env.DEV) {
    throw new Error(
      'VITE_SERVER_URL is not set. Run `npm run dev` (predev derives it) or set it in .env.development.local.'
    )
  }
  const res = await fetch('/config.json', { cache: 'no-store' })
  if (!res.ok) {
    throw new Error(`config.json HTTP ${res.status}`)
  }
  const pointer = (await res.json()) as ConfigPointer
  if (!pointer.SERVER_URL) {
    throw new Error('config.json is missing SERVER_URL')
  }
  return pointer.SERVER_URL
}

/**
 * Fetch and map the runtime config. Throws on any network/parse failure so that
 * TanStack Query can retry and AppBootstrap can render an error + retry state —
 * we never silently degrade to an empty config.
 */
export async function fetchRuntimeConfig(): Promise<RuntimeConfig> {
  const url = await resolveClientOutputsUrl()
  const res = await fetch(url, { cache: 'no-store' })
  if (!res.ok) {
    throw new Error(`client-outputs HTTP ${res.status}`)
  }
  return {
    ...mapClientOutputs((await res.json()) as ClientOutputs),
    SERVER_URL: url,
  }
}
