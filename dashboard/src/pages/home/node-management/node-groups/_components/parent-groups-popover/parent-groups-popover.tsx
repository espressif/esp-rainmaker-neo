/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ChevronDown,
  ChevronRight,
  SquaresSubtract,
  Group,
} from "lucide-react";
import {
  Badge,
  List,
  Popover,
  PopoverContent,
  PopoverTrigger,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import type { ListGroup } from "@espressif/dashboard-ui-components/components";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import type { ParentGroupsPopoverProps } from "./parent-groups-popover.props";

export function ParentGroupsPopover({
  parentGroupNames,
}: ParentGroupsPopoverProps) {
  const { t } = useTranslation("node-groups");
  const [open, setOpen] = useState(false);

  // One group holds every parent, so no dividers are drawn between rows — `List`
  // only separates groups. The popover heading rides on the group label.
  const parentGroups = useMemo<ListGroup[]>(
    () => [
      {
        id: "parent-groups",
        labelClassName: "text-xs tracking-wide text-muted-foreground",
        items: parentGroupNames.map((groupName) => ({
          id: groupName,
          label: groupName,
          startIcon: <Group className="h-4 w-4 shrink-0" aria-hidden />,
          endIcon: <ChevronRight className="h-4 w-4 shrink-0" aria-hidden />,
          // `List` forwards no route params, so the path is interpolated here.
          href: `/home/node-management/node-groups/${groupName}`,
          color: "gray",
        })),
      },
    ],
    [parentGroupNames],
  );

  if (parentGroupNames.length === 0) {
    return null;
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          onClick={(e) => e.stopPropagation()}
          className="inline-flex items-center"
        >
          <Badge
            color="secondary"
            variant="gradient"
            className="font-normal gap-2 cursor-pointer px-2 py-1"
          >
            <SquaresSubtract className="h-3.5 w-3.5 shrink-0" aria-hidden />
            {t("subgroup.label", "Sub-group")}
            <ChevronDown className="h-3.5 w-3.5 shrink-0" aria-hidden />
          </Badge>
        </button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-72"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Rows carry `href`, so they navigate on their own — this handler only
            dismisses the popover. It is needed because moving between two
            group-details pages keeps this component mounted, which would
            otherwise leave the popover open on the new group. */}
        <div onClick={() => setOpen(false)}>
          <p className="text-xs tracking-wide text-muted-foreground leading-relaxed">
            {t("subgroup.parentsHeading", "Parent groups")}
          </p>

          <SectionCard
            color="silver"
            variant="soft"
            className="[&_.section-card-body]:p-0 mt-2"
            allowCollapse={false}
          >
            <List
              items={parentGroups}
              linkComponent={TanstackRouterLink}
              showSeparators={true}
              itemClassName="border-b border-border rounded-none hover:bg-transparent last:border-b-0"
              separatorClassName="m-0"
            />
          </SectionCard>
        </div>
      </PopoverContent>
    </Popover>
  );
}
