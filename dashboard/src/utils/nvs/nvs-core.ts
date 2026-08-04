/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * NVS partition generation — runtime-agnostic core.
 *
 * Owns the NVS row model, the version-pinned `esp-idf-nvs-partition-gen`
 * requirement, the shared Python packing routine, and the row→binary packing
 * given an (already-initialised) Pyodide instance. It does NOT load Pyodide —
 * that is the job of the runtime adapters (`pyodide-browser.ts` for the
 * dashboard UI, `pyodide-node.ts` for the CLI), which both layer on top of
 * this. Producers (RainMaker bulk-generate, Matter matter-gen) only build
 * `NvsRow[]` and hand them to a runtime adapter.
 *
 * This file has no browser- or Node-specific imports, so it is safe in any
 * runtime and unit-testable directly.
 */

/*
 * Pyodide drives a dynamically-typed Python runtime: `pyimport`, `globals.set`
 * and `runPython` return Python proxies whose shape is only known at runtime.
 * These stay `any` deliberately — hand-written types here would be fiction that
 * the compiler could not check against the actual Python module.
 */
/* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-call, @typescript-eslint/no-unsafe-member-access, @typescript-eslint/no-unsafe-assignment, @typescript-eslint/no-unsafe-return */

export type NvsEncoding = '' | 'u32' | 'string' | 'binary' | 'hex2bin'

export interface NvsRow {
  /** For a namespace marker this is the namespace name. */
  key: string
  type: 'namespace' | 'data' | 'file'
  encoding: NvsEncoding
  value: string | number | Uint8Array
}

/**
 * Python requirement installed into Pyodide via micropip for NVS packing.
 * Pinned for byte-parity with rmng-sdk factory_autoreg. Keep in sync with
 * matter-gen/requirements.txt.
 */
export const NVS_PARTITION_GEN_REQUIREMENT = 'esp-idf-nvs-partition-gen==0.2.0'

/** Default RainMaker manufacturing partition size (12 KB). */
export const RMAKER_PARTITION_SIZE = 0x3000
/** Default merged Matter + RainMaker factory partition size (24 KB). */
export const MATTER_PARTITION_SIZE = 0x6000

type WireOp = 'ns' | 'u32' | 'str' | 'utf8bin' | 'rawbin' | 'hex2bin'

interface WireRow {
  key: string
  op: WireOp
  /** Always a string so the row list crosses into Python as plain JSON. */
  val: string
}

function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

/**
 * Normalize structured NVS rows to a flat, JSON-safe "wire" form.
 *
 * Every value becomes a string and every row carries an unambiguous op, so the
 * row list can be handed to Python as JSON (no JsProxy / bytes-marshalling
 * surprises). `binary` splits by value kind:
 *   - string  → utf8bin (UTF-8 bytes, as the RainMaker flow has always stored)
 *   - Uint8Array → rawbin (raw bytes via hex, e.g. DER blobs)
 *
 * Exported for unit testing; the actual packing only runs under Pyodide.
 */
export function toWireRows(rows: NvsRow[]): WireRow[] {
  return rows.map((r): WireRow => {
    if (r.type === 'namespace') {return { key: r.key, op: 'ns', val: '' }}
    switch (r.encoding) {
      case 'u32':
        return { key: r.key, op: 'u32', val: String(r.value) }
      case 'string':
        return { key: r.key, op: 'str', val: String(r.value) }
      case 'hex2bin':
        return { key: r.key, op: 'hex2bin', val: String(r.value) }
      case 'binary':
        return r.value instanceof Uint8Array
          ? { key: r.key, op: 'rawbin', val: bytesToHex(r.value) }
          : { key: r.key, op: 'utf8bin', val: String(r.value) }
      default:
        throw new Error(`unsupported NVS encoding '${r.encoding}' for key '${r.key}'`)
    }
  })
}

/**
 * Shared Python packing routine. Assumes `rows_json` (JSON of toWireRows) and
 * `input_size` (int) are defined, and leaves the partition bytes in `buf`.
 */
export const NVS_PACK_PYTHON = `import io, json
from esp_idf_nvs_partition_gen.nvs_partition_gen import nvs_open, write_entry, nvs_close, Page

rows = json.loads(rows_json)
buf = io.BytesIO()
nvs = nvs_open(buf, input_size, version=Page.VERSION2)

for r in rows:
    key, op, val = r['key'], r['op'], r['val']
    if op == 'ns':
        write_entry(nvs, key, 'namespace', '', '')
    elif op == 'u32':
        write_entry(nvs, key, 'data', 'u32', val)
    elif op == 'str':
        write_entry(nvs, key, 'data', 'string', val)
    elif op == 'utf8bin':
        write_entry(nvs, key, 'data', 'binary', val.encode('utf-8'))
    elif op == 'rawbin':
        write_entry(nvs, key, 'data', 'binary', bytes.fromhex(val))
    elif op == 'hex2bin':
        write_entry(nvs, key, 'data', 'hex2bin', val)
    else:
        raise ValueError('unknown NVS wire op: ' + op)

nvs_close(nvs)
`

/** Install the pinned esp-idf-nvs-partition-gen package into a Pyodide instance. */
export async function installNvsPackage(pyodide: any): Promise<void> {
  await pyodide.loadPackage('micropip')
  const micropip = pyodide.pyimport('micropip')
  await micropip.install(NVS_PARTITION_GEN_REQUIREMENT)
}

/**
 * Pack rows into a partition binary using an already-initialised Pyodide
 * instance (with esp-idf-nvs-partition-gen installed). Runtime-agnostic, so the
 * browser and Node adapters produce byte-identical binaries.
 * `partitionSize` is the total partition size (one page is reserved internally).
 */
export function packRowsWithPyodide(
  pyodide: any,
  rows: NvsRow[],
  partitionSize: number,
): Uint8Array {
  pyodide.globals.set('rows_json', JSON.stringify(toWireRows(rows)))
  pyodide.globals.set('input_size', partitionSize - 4096)
  const resultBytes = pyodide.runPython(`${NVS_PACK_PYTHON}\nbuf.getvalue()`)
  return new Uint8Array(resultBytes.toJs())
}
