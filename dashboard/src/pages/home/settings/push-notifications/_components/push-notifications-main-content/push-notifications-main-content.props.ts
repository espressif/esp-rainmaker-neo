/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { IntegrationDetail } from "@/api/integrations";

export interface PushNotificationsMainContentProps {
  integrations: IntegrationDetail[];
  isLoading: boolean;
  error: Error | null;
  onAddIntegration: () => void;
}
