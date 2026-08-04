/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { InternalPageHeaderProps } from "./internal-page-header.props";

export const DEFAULT_BACK_LABEL = "Back";

export function hasRenderableContent(value: unknown): boolean {
  if (value == null || value === "" || typeof value === "boolean") {
    return false;
  }

  return !(typeof value === "string" && value.trim() === "");
}

export interface InternalPageHeaderVisibility {
  showBackLink: boolean;
  backLinkHref: string;
  backLinkDisplayLabel: string;
  showMetaStrip: boolean;
  resourceLabel: string;
  resourceId: string;
  showTitleRow: boolean;
  showAvatar: boolean;
  showHeading: boolean;
  showDescription: boolean;
  showActions: boolean;
  showFooter: boolean;
}

export function resolveInternalPageHeaderVisibility(
  props: InternalPageHeaderProps,
): InternalPageHeaderVisibility {
  const hrefTrimmed = props.backLinkHref?.trim() ?? "";
  const backLabelTrimmed = props.backLinkLabel?.trim() ?? "";
  const labelTrimmed = props.resourceLabel?.trim() ?? "";
  const idTrimmed = props.resourceId?.trim() ?? "";
  const showAvatar = hasRenderableContent(props.avatar);
  const showHeading = hasRenderableContent(props.heading);
  const showDescription = hasRenderableContent(props.description);
  const showActions = hasRenderableContent(props.actions);
  const showFooter = hasRenderableContent(props.footer);

  return {
    showBackLink: hrefTrimmed !== "",
    backLinkHref: hrefTrimmed,
    backLinkDisplayLabel:
      backLabelTrimmed !== "" ? backLabelTrimmed : DEFAULT_BACK_LABEL,
    showMetaStrip:
      labelTrimmed !== "" ||
      idTrimmed !== "" ||
      hasRenderableContent(props.metaEnd),
    resourceLabel: labelTrimmed,
    resourceId: idTrimmed,
    showTitleRow:
      showAvatar || showHeading || showDescription || showActions,
    showAvatar,
    showHeading,
    showDescription,
    showActions,
    showFooter,
  };
}
