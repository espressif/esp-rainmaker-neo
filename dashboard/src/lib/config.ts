/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Runtime application configuration.
 *
 * Every value comes from the published client-outputs document, fetched at
 * startup (see `@/api/config`) and cached in `@/stores/config.store` — the
 * single source of truth these getters read from. The getters stay synchronous
 * and imperative so non-React callers (axios interceptors, SigV4 signing, AWS
 * SDK clients) can read config at call time.
 */

import { useConfigStore } from '@/stores/config.store'

export interface RuntimeConfig {
  API_GATEWAY_URL?: string
  ESP_USER_API_URL?: string
  COGNITO_USER_POOL_ID?: string
  COGNITO_CLIENT_ID?: string
  REGION?: string
  IOT_ENDPOINT?: string
  OTA_S3_BUCKET?: string
  OTA_SERVICE_ROLE_ARN?: string
  CREDENTIAL_PROVIDER_ENDPOINT?: string
  FILES_BUCKET_NAME?: string
  /** Admin Cognito OAuth client id; espuser-base `EspAdminUserPoolClientId`. */
  OIDC_CLIENT_ID?: string
  /** OAuth authorization endpoint; espuser-base `EspUserAuthorizeUrl`. */
  AUTHORIZE_URL?: string
  /** OAuth token endpoint; espuser-base `EspUserTokenUrl`. */
  TOKEN_URL?: string
  /** OAuth client id the voice assistants link as; espuser-base `EspVaClientId`. */
  VA_CLIENT_ID?: string
  /** Google voice assistant fulfillment endpoint; rmng-gva-core `GVAFulfillmentUrl`. */
  GVA_FULFILLMENT_URL?: string
  /** Whether the optional `rmng-gva-core` stack is deployed. */
  GVA_ENABLED?: boolean
  /** Whether the Alexa configuration stack (`rmng-alexa-cfg-core`) is deployed. */
  ALEXA_ENABLED?: boolean
  /** Whether the SmartThings configuration stack (`rmng-st-cfg-core`) is deployed. */
  SMARTTHINGS_ENABLED?: boolean
  /** Alexa skill endpoint ARN per Alexa region; the per-region `rmng-alexa-core` stacks. */
  ALEXA_SKILL_ARNS?: Record<string, string>
  /** Whether the optional `rmng-bridge-core` stack is deployed. */
  BRIDGE_ENABLED?: boolean
  /** URL of the published client-outputs document this config was fetched from. */
  SERVER_URL?: string
}

function fromConfig(key: keyof RuntimeConfig): string | undefined {
  const value = useConfigStore.getState().config?.[key]
  return typeof value === 'string' && value.length > 0 ? value : undefined
}

/**
 * The OAuth authorization endpoint. A voice assistant's account linking is pointed at it, so the
 * settings pages surface it verbatim. It answers a signed request carrying a client and redirect
 * URI — it is a value to configure, not a page to open.
 */
export function getAuthorizeUrl(): string {
  return fromConfig('AUTHORIZE_URL') ?? ''
}

/** The OAuth token endpoint, the other half of a voice assistant's account linking config. */
export function getTokenUrl(): string {
  return fromConfig('TOKEN_URL') ?? ''
}

/**
 * The OAuth client a voice assistant authenticates as. It is a confidential client, but only
 * its id lives here — the client-outputs document is served anonymously readable, so the
 * matching secret is fetched from the authenticated admin API instead.
 */
export function getVaClientId(): string {
  return fromConfig('VA_CLIENT_ID') ?? ''
}

/** The endpoint Google's Home Graph delivers voice intents to. */
export function getGvaFulfillmentUrl(): string {
  return fromConfig('GVA_FULFILLMENT_URL') ?? ''
}

/**
 * The skill endpoint ARN per Alexa region, keyed by the AWS region that Alexa region maps to. A
 * Smart Home skill is configured with one endpoint per region it serves, so all of them are
 * surfaced rather than a single default.
 */
export function getAlexaSkillArns(): Record<string, string> {
  const value = useConfigStore.getState().config?.ALEXA_SKILL_ARNS
  return value && typeof value === 'object' ? value : {}
}

/** Whether the Alexa configuration stack (`rmng-alexa-cfg-core`) is deployed, i.e. the integration is available. */
export function isAlexaEnabled(): boolean {
  return useConfigStore.getState().config?.ALEXA_ENABLED === true
}

/** Whether the Google Home stack (`rmng-gva-core`) is deployed, i.e. the integration is available. */
export function isGvaEnabled(): boolean {
  return useConfigStore.getState().config?.GVA_ENABLED === true
}

/** Whether the SmartThings configuration stack (`rmng-st-cfg-core`) is deployed, i.e. the integration is available. */
export function isSmartThingsEnabled(): boolean {
  return useConfigStore.getState().config?.SMARTTHINGS_ENABLED === true
}

/** Whether the optional bridge stack (`rmng-bridge-core`) is deployed, i.e. the bridge capability is available. */
export function isBridgeEnabled(): boolean {
  return useConfigStore.getState().config?.BRIDGE_ENABLED === true
}

/** Get ESP User API base URL. */
export function getEspUserApiUrl(): string {
  return fromConfig('ESP_USER_API_URL') ?? ''
}

/** Get API Gateway base URL. */
export function getApiGatewayUrl(): string {
  return fromConfig('API_GATEWAY_URL') ?? ''
}

/** Get AWS region (defaults to us-east-1 before config resolves). */
export function getAwsRegion(): string {
  return fromConfig('REGION') ?? 'us-east-1'
}

/** Admin Cognito user pool id. */
export function getCognitoUserPoolId(): string {
  return fromConfig('COGNITO_USER_POOL_ID') ?? ''
}

/** Admin Cognito app client id. */
export function getCognitoClientId(): string {
  return fromConfig('COGNITO_CLIENT_ID') ?? ''
}

/** Get IoT Data-ATS endpoint hostname. */
export function getIotEndpoint(): string {
  return fromConfig('IOT_ENDPOINT') ?? ''
}

/** Get OTA S3 bucket name. */
export function getOtaS3Bucket(): string {
  return fromConfig('OTA_S3_BUCKET') ?? ''
}

/** Get OTA service role ARN (used by AWS IoT to access S3 firmware and create jobs). */
export function getOtaServiceRoleArn(): string {
  return fromConfig('OTA_SERVICE_ROLE_ARN') ?? ''
}

// Hostname devices hit to obtain temporary AWS credentials via X.509 mTLS (rmng-files feature).
export function getCredentialProviderEndpoint(): string {
  return fromConfig('CREDENTIAL_PROVIDER_ENDPOINT') ?? ''
}

// Same physical bucket as OTA_S3_BUCKET today, but kept separate so the two can diverge without touching call sites.
export function getFilesBucket(): string {
  return fromConfig('FILES_BUCKET_NAME') ?? ''
}

/** URL of the published client-outputs document (dev: `VITE_SERVER_URL`; prod: `/config.json`). */
export function getServerUrl(): string {
  return fromConfig('SERVER_URL') ?? ''
}
