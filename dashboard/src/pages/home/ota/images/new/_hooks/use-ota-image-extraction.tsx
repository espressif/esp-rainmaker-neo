/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useEffect, useRef, useState } from "react";
import type { UseFormReturn } from "react-hook-form";
import {
  ESP_IMAGE_PARSE_LENGTH,
  parseEspAppImage,
} from "@/utils/esp-image/esp-image";
import {
  computeOtaImagePrefill,
  lockedFieldsFor,
  NO_LOCKED_OTA_IMAGE_FIELDS,
  type OtaImageLockedFields,
  type OtaImagePrefillField,
  type OtaImagePrefillFields,
} from "../_utils/ota-image-prefill";
import type { UploadOtaImageFormValues } from "../_schema/upload-ota-image-form.schema";

const NO_PREFILL: Partial<OtaImagePrefillFields> = {};

/**
 * Parses the selected firmware file's ESP app-image header, fills the fields the
 * image speaks for — version, model, platform — and reports them as locked so
 * they cannot be edited away from what the binary reports. Non-ESP files
 * (.hex/.elf, MCU images) yield no extraction and leave every field editable.
 */
export function useOtaImageExtraction(
  form: UseFormReturn<UploadOtaImageFormValues>,
) {
  const [lockedFields, setLockedFields] = useState<OtaImageLockedFields>(
    NO_LOCKED_OTA_IMAGE_FIELDS,
  );
  const filledFields = useRef<OtaImagePrefillField[]>([]);

  const applyPrefill = useCallback(
    (values: Partial<OtaImagePrefillFields>) => {
      const fields = Object.keys(values) as OtaImagePrefillField[];
      // Fields the previous file filled but this one does not report are cleared, so swapping images never strands a stale value in a field the admin can no longer edit.
      for (const field of new Set([...filledFields.current, ...fields])) {
        form.setValue(field, values[field] ?? "", { shouldDirty: true });
      }
      filledFields.current = fields;
      setLockedFields(lockedFieldsFor(values));
    },
    [form],
  );

  const files = form.watch("firmwareFiles");
  const file = files[0];

  useEffect(() => {
    if (!file) {
      applyPrefill(NO_PREFILL);
      return;
    }
    let cancelled = false;
    // Only the image header and app descriptor are needed, never the full file.
    void file
      .slice(0, ESP_IMAGE_PARSE_LENGTH)
      .arrayBuffer()
      .then((buffer) => {
        if (cancelled) {
          return;
        }
        const result = parseEspAppImage(buffer);
        applyPrefill(
          result.ok ? computeOtaImagePrefill(result.info) : NO_PREFILL,
        );
      })
      .catch(() => {
        if (!cancelled) {
          applyPrefill(NO_PREFILL);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [file, applyPrefill]);

  return { lockedFields };
}
