/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { MoveLeft } from "lucide-react";
import { Link } from "@espressif/dashboard-ui-components/components";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import { cn } from "@/utils/utils";
import InternalPageHeaderMetaStrip from "./_components/internal-page-header-meta-strip";
import InternalPageHeaderTitleRow from "./_components/internal-page-header-title-row";
import type { InternalPageHeaderProps } from "./internal-page-header.props";
import { resolveInternalPageHeaderVisibility } from "./internal-page-header.utils";

export default function InternalPageHeader(props: InternalPageHeaderProps) {
  const visibility = resolveInternalPageHeaderVisibility(props);

  return (
    <div
      className={cn(
        "flex w-full min-w-0 flex-col text-base font-normal tracking-normal",
        visibility.showBackLink && "gap-3",
        props.className,
      )}
    >
      {visibility.showBackLink ? (
        <div>
          <Link
            to={visibility.backLinkHref}
            linkComponent={TanstackRouterLink}
            color="gray"
            startIcon={<MoveLeft aria-hidden />}
          >
            {visibility.backLinkDisplayLabel}
          </Link>
        </div>
      ) : null}

      {visibility.showMetaStrip ? (
        <InternalPageHeaderMetaStrip
          resourceLabel={visibility.resourceLabel}
          resourceId={visibility.resourceId}
          metaEnd={props.metaEnd}
        />
      ) : null}

      {visibility.showTitleRow ? (
        <InternalPageHeaderTitleRow
          visibility={visibility}
          avatar={props.avatar}
          heading={props.heading}
          description={props.description}
          actions={props.actions}
        />
      ) : null}

      {visibility.showFooter ? (
        <div className="px-5 pt-3">{props.footer}</div>
      ) : null}
    </div>
  );
}
