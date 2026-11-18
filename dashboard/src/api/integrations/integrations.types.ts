/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */


export interface AlexaConfigGetResponse {
  client_id?: string
  skill_id?: string
  redirect_uris?: string[]

  manufacturer_name?: string
  /** The last automatic setup's status; absent when none has run. */
  config_status?: AlexaSetupStatus
  /** Security profile of the connected Amazon developer account; absent when none is connected. */
  lwa_client_id?: string
}

export type AlexaSetupState = 'IN_PROGRESS' | 'DONE' | 'FAILED'

/** Progress of an automatic setup. ref_id is the skill id, carried on a failure too so the orphaned skill can be found. */
export interface AlexaSetupStatus {
  status: AlexaSetupState
  ref_id?: string
  stage?: string
  error?: string
  updated_at: string
}

export interface AlexaConfigRequest {
  redirect_uris: string[]
  client_id: string
  client_secret: string
  skill_id: string

  manufacturer_name?: string
}

export interface AlexaConfigResponse {
  status: string
}

/**
 * Automatic mode: the backend creates the skill, configures it at Amazon and configures Neo to use it, signed in as the Amazon account connected through {@link AlexaLwaConnectionRequest}. manufacturer_name absent keeps the stored brand.
 */
export interface AlexaAutoConfigurationRequest {
  skill_name: string
  manufacturer_name?: string
}

/** Connects an Amazon developer account: the backend redeems the one-time code from Amazon's consent popup and stores the resulting refresh token. */
export interface AlexaLwaConnectionRequest {
  lwa_client_id: string
  lwa_client_secret: string
  lwa_authorization_code: string
  /** Must equal the redirect_uri the code was issued for, or Amazon refuses the exchange. */
  lwa_redirect_uri: string
}

export interface AlexaLwaConnectionResponse {
  message: string
}

/**
 * Returned as soon as the skill exists. Configuration (builds, account linking, stored
 * credentials) continues in the background, so status is IN_PROGRESS rather than done.
 */
export interface AlexaAutoConfigurationResponse {
  skill_id: string
  status: string
}

export interface GvaServiceAccount {
  type?: string
  project_id: string
  private_key_id?: string
  private_key: string
  client_email: string
  client_id?: string
  auth_uri?: string
  token_uri?: string
  auth_provider_x509_cert_url?: string
  client_x509_cert_url?: string
  universe_domain?: string
}

export interface GvaConfigGetResponse extends Omit<GvaServiceAccount, 'private_key'> {
  redirect_uris?: string[]
}

export type GvaConfigRequest = GvaServiceAccount

export interface GvaConfigResponse {
  message: string
  status: string
  project_id: string
  redirect_uris: string[]
}

export interface GvaConfigDeleteResponse {
  message: string
  status: string
}

export interface SmartThingsConfigGetResponse {
  client_id?: string
}

export interface SmartThingsConfigRequest {
  client_id: string
  client_secret: string
}

export interface SmartThingsConfigResponse {
  message: string
}

export type SmartThingsConfigDeleteResponse = SmartThingsConfigResponse

export type PushIntegrationType = 'apns' | 'apns_sandbox' | 'gcm'

export const PUSH_INTEGRATION_TYPES: PushIntegrationType[] = ['apns', 'apns_sandbox', 'gcm']

export interface PushIntegrationRequest extends Partial<GvaServiceAccount> {
  authentication_key?: string
  key_id?: string
  team_id?: string
  bundle_id?: string
}

export interface IntegrationDetail extends Partial<Omit<GvaServiceAccount, 'private_key'>> {
  integration_id: string
  integration_type: string
  bundle_id?: string
}

export interface ListIntegrationsResponse {
  integrations: IntegrationDetail[]
}

export interface RegisterIntegrationResponse {
  integration_id: string
}

export interface IntegrationStatusResponse {
  status: string
  description?: string
}
