/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";
import { useGetGvaConfig } from "@/api/integrations";
import { GvaMainContent } from "./_components/gva-main-content";
import { GvaConfigFormSheet } from "./_components/gva-config-form-sheet";

export default function Gva() {
  const { data, isLoading, error } = useGetGvaConfig();
  const [isFormOpen, setIsFormOpen] = useState(false);

  return (
    <div className="py-6">
      <GvaMainContent
        data={data}
        isLoading={isLoading}
        error={error}
        onConfigure={() => setIsFormOpen(true)}
      />

      {isFormOpen && (
        <GvaConfigFormSheet
          initialData={data}
          onClose={() => setIsFormOpen(false)}
        />
      )}
    </div>
  );
}
