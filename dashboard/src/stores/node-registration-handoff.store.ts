/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { create } from 'zustand'

/**
 * In-memory handoff channel for carrying a generated `node_certs.csv` File from
 * the "Generate test nodes" flow to the "Register nodes" page, so the CSV can be
 * pre-uploaded instead of downloaded and re-selected by hand.
 *
 * Deliberately NOT persisted: a File cannot be serialized to storage, and the
 * handoff is meant to survive a single in-app navigation, not a reload.
 */
interface NodeRegistrationHandoffState {
  pendingCsvFile: File | null
  setPendingCsvFile: (file: File | null) => void
  clearPendingCsvFile: () => void
}

export const useNodeRegistrationHandoffStore = create<NodeRegistrationHandoffState>((set) => ({
  pendingCsvFile: null,
  setPendingCsvFile: (file) => set({ pendingCsvFile: file }),
  clearPendingCsvFile: () => set({ pendingCsvFile: null }),
}))
