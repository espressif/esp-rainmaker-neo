/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { RegistrationJobStatus } from "@/config/registration-job-status.config";

export interface RegisterPageHeaderProps {
  statusFilter: RegistrationJobStatus | null;
  onStatusFilterChange: (status: RegistrationJobStatus | null) => void;
  onRegisterClick: () => void;
}
