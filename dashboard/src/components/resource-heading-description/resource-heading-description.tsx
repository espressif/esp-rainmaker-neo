/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ResourceHeadingDescriptionProps } from "./resource-heading-description.props";

/**
 * `description` slot for the resource details page headings: a status badge
 * stacked above the resource's own description.
 */
export default function ResourceHeadingDescription({
  badge,
  description,
}: ResourceHeadingDescriptionProps) {
  if (!badge && !description) {
    return null;
  }

  return (
    <div className="flex flex-col items-start gap-1.5">
      {badge}
      {description ? <span>{description}</span> : null}
    </div>
  );
}
