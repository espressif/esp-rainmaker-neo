/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { GvaEditMethod } from "../../use-gva-config-form";

export interface GvaEditMethodFieldProps {
  /** Currently selected entry method. */
  value: GvaEditMethod;
  /** Called when the user switches between upload and manual entry. */
  onChange: (value: GvaEditMethod) => void;
}
