/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";
import { useGetSmartThingsConfig } from "@/api/integrations";
import { SmartThingsMainContent } from "./_components/smartthings-main-content";
import { SmartThingsConfigFormSheet } from "./_components/smartthings-config-form-sheet";

export default function SmartThings() {
  const { data, isLoading, error } = useGetSmartThingsConfig();
  const [isFormOpen, setIsFormOpen] = useState(false);

  return (
    <div className="py-6">
      <SmartThingsMainContent
        data={data}
        isLoading={isLoading}
        error={error}
        onConfigure={() => setIsFormOpen(true)}
      />

      {isFormOpen && (
        <SmartThingsConfigFormSheet
          initialData={data}
          onClose={() => setIsFormOpen(false)}
        />
      )}
    </div>
  );
}
