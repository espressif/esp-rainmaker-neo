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
