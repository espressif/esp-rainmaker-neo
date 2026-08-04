/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface ConfigurationValuesListProps {
  /** OAuth authorization endpoint, empty when the platform has not published one. */
  authorizeUrl: string;
  /** OAuth token endpoint, empty when the platform has not published one. */
  tokenUrl: string;
  /** Voice-assistant OAuth client id. */
  clientId: string;
  /** Client secret, present only once the OAuth client has been read back. */
  secret?: string;
  /** Space-separated scope list, as both developer consoles expect it. */
  scopes?: string;
  /** Google-only fulfillment URL; empty on the Alexa tab. */
  fulfillmentUrl: string;
  /** Alexa-only skill endpoint ARNs keyed by region; empty on the GVA tab. */
  skillArns: Record<string, string>;
}
