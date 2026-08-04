/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import ValueDetailList from "../value-detail-list";
import type { FixedValueContentProps } from "./fixed-value-content.props";

/**
 * A limit AWS fixes for every account. It cannot load or fail, so it deliberately
 * does not go through the query layer at all.
 */
export default function FixedValueContent({ value }: FixedValueContentProps) {
  return <ValueDetailList reading={value.reading} noteKey={value.noteKey} />;
}
