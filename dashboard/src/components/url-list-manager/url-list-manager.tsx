/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useSetState } from "react-use";
import { Plus, Trash2 } from "lucide-react";
import {
  Alert,
  Button,
  CopiableText,
  IconTextActionCard,
  Input,
  Popover,
  PopoverContent,
  PopoverTrigger,
  SectionCard,
  Typography,
} from "@espressif/dashboard-ui-components/components";
import type { UrlListManagerProps } from "./url-list-manager.props";

const ACTION_ICON_CLASS = "h-4 w-4 shrink-0";

interface AddUrlPopoverState {
  isOpen: boolean;
  value: string;
  error: string | null;
}

const INITIAL_POPOVER_STATE: AddUrlPopoverState = {
  isOpen: false,
  value: "",
  error: null,
};

/**
 * Pure, controlled manager for a list of URLs (redirect URIs, callback URLs,
 * webhook endpoints, …). It takes `value` / `onChange` and renders — no form
 * library involved, so it works with plain `useState`, a store, or wrapped by a
 * form adapter (see {@link "../url-list-field"}). Mirrors the v2
 * CallbackUrlsSectionCard UX: add via a popover (non-empty + no-duplicate
 * check), list as cards with a delete action. All copy is passed in so the same
 * component serves any URL type.
 */
export default function UrlListManager({
  value,
  onChange,
  cardTitle,
  cardDescription,
  icon,
  labels,
  error,
  readOnly = false,
}: UrlListManagerProps) {
  const [popover, setPopover] = useSetState<AddUrlPopoverState>(
    INITIAL_POPOVER_STATE,
  );

  const closePopover = () => setPopover(INITIAL_POPOVER_STATE);

  const handleOpenChange = (isOpen: boolean) => {
    if (isOpen) {
      setPopover({ isOpen: true });
      return;
    }
    closePopover();
  };

  const handleAdd = () => {
    const trimmed = popover.value.trim();
    if (!trimmed) {
      setPopover({ error: labels.requiredError });
      return;
    }
    if (value.includes(trimmed)) {
      setPopover({ error: labels.duplicateError });
      return;
    }
    onChange([...value, trimmed]);
    closePopover();
  };

  const handleRemove = (index: number) => {
    onChange(value.filter((_, itemIndex) => itemIndex !== index));
  };

  return (
    <SectionCard
      icon={icon}
      primaryText={cardTitle}
      secondaryText={cardDescription}
      allowCollapse={false}
      size="sm"
      variant="outline"
      color="mist"
      actions={
        readOnly ? undefined : (
        <Popover open={popover.isOpen} onOpenChange={handleOpenChange}>
          <PopoverTrigger asChild>
            <Button
              type="button"
              size="sm"
              variant="outline"
              color="mist"
              fullWidth={false}
              startIcon={<Plus className={ACTION_ICON_CLASS} aria-hidden />}
            >
              {labels.addAction}
            </Button>
          </PopoverTrigger>
          <PopoverContent align="end" className="w-80">
            <div className="space-y-3">
              <Input
                label={labels.inputLabel}
                placeholder={labels.inputPlaceholder}
                value={popover.value}
                error={!!popover.error}
                onChange={(event) =>
                  setPopover({ value: event.target.value, error: null })
                }
              />
              {popover.error ? (
                <Typography variant="body2" as="p" className="text-destructive">
                  {popover.error}
                </Typography>
              ) : null}
              <div className="flex items-center justify-end gap-2">
                <Button
                  type="button"
                  size="sm"
                  variant="default"
                  color="mist"
                  fullWidth={false}
                  onClick={closePopover}
                >
                  {labels.cancelAction}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="default"
                  color="primary"
                  fullWidth={false}
                  onClick={handleAdd}
                >
                  {labels.addAction}
                </Button>
              </div>
            </div>
          </PopoverContent>
        </Popover>
        )
      }
    >
      {value.length === 0 ? (
        <Alert
          type={error ? "error" : "info"}
          color={error ? "error" : "info"}
          variant="soft"
          hideIcon
        >
          {error ?? labels.emptyState}
        </Alert>
      ) : (
        <div className="space-y-3">
          {value.map((url, index) => (
            <IconTextActionCard
              key={url}
              icon={icon}
              description={
                <CopiableText text={url} className="text-sm font-normal" />
              }
              actions={
                readOnly ? undefined : (
                  <Button
                    type="button"
                    size="icon"
                    variant="ghost"
                    color="error"
                    aria-label={labels.deleteAriaLabel}
                    onClick={() => handleRemove(index)}
                  >
                    <Trash2 className="h-4 w-4" aria-hidden />
                  </Button>
                )
              }
              color="mist"
              size="sm"
              variant="soft"
            />
          ))}
          {error ? (
            <Typography variant="body2" as="p" className="text-destructive">
              {error}
            </Typography>
          ) : null}
        </div>
      )}
    </SectionCard>
  );
}
