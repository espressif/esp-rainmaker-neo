# matter-gen

Standalone, in-browser/Node TypeScript generator for **Matter factory data**.

It reproduces what Espressif's `esp-matter-mfg-tool` + `factory_nvs_gen`
(`rmng-sdk/extras/tools/factory_autoreg`) emit per device — but with **no
native binaries** (`chip-cert` / `spake2p`) and **no Python**, so it can run
client-side in the admin dashboard exactly like the existing RainMaker bulk
generator.

This module is intentionally **decoupled from the dashboard UI** so it can be
tested in isolation first, then integrated.

## What it produces (per device)

| Artifact | Source |
| --- | --- |
| DAC certificate + key (P-256) | `matter-cert.ts` |
| DAC signed by the bundled CHIP test PAI (FFF2/8001) | `matter-cert.ts` + `chip-test-credentials.ts` |
| Discriminator, passcode (spec-valid) | `index.ts` |
| Salt + SPAKE2+ verifier | `spake2p.ts` |
| Onboarding QR string (`MT:…`) + URL | `setup-payload.ts` / `base38.ts` |
| Manual pairing code (Verhoeff-checked) | `setup-payload.ts` / `verhoeff.ts` |
| `chip-factory` NVS rows (+ optional merged `rmaker_creds`) | `chip-factory-nvs.ts` |

The DAC subject CN is a UUIDv4 and becomes the cloud **thing name** (matches
`factory_autoreg`). The final NVS `.bin` packing is **not** done here — the rows
are handed to the shared NVS engine in `util/nvs/` (`generateNvsBin` for
the browser, `generateNvsBinNode` for the CLI).

## Usage

```ts
import { generateMatterDevice } from '@/utils/matter-gen'

const device = generateMatterDevice({
  vendorId: 0xfff2,
  productId: 0x8001,
  mqttHost: 'xxxx-ats.iot.us-east-1.amazonaws.com', // merges rmaker_creds rows
  // pai: { certPem, keyPem },  // override; default = bundled CHIP test PAI for VID/PID
  // cdDer: <Uint8Array>,       // Certification Declaration
})

device.thingName          // DAC CN (UUIDv4) — register this node
device.dac.certPem        // register as `cert`
device.pai.certPem        // register as `ca_cert`
device.qrUrl              // onboarding QR link
device.manualPairingCode  // 11-digit manual code
device.nvsRows            // -> nvs-partition-gen
```

## CLI (factory_autoreg parity)

`cli.ts` generates the same per-device artifacts as
`rmng-sdk/extras/tools/factory_autoreg/factory_autoreg.py --matter`, driven by
this library. The NVS `.bin` is packed with **Pyodide-in-Node** — the identical
WASM runtime and `esp-idf-nvs-partition-gen` module the dashboard UI runs in the
browser (via the shared `packRowsWithPyodide`) — so the binaries are
byte-equivalent and **no system Python is required**. The first run
micropip-installs the package (needs network); wheels are then cached under
`node_modules` for subsequent runs.

```bash
# from dashboard — VID/PID default to FFF2/8001 (bundled test PAI+CD)
npm run matter-gen -- --matter -n 3 \
  --output-dir ./outputs --account-id 123456789012 \
  --mqtt-host xxxx-ats.iot.us-east-1.amazonaws.com

npm run matter-gen -- --help        # all options
```

Per-device output (`<output-dir>/<account-id>/matter/<thing-name>/`):
`dac_key.pem`, `dac_cert.pem`, `pai_cert.pem`, `qr_link.txt`,
`factory_nvs_input.json`, `registration.json`, `esp-idf/<part-label>.bin`
(+ `batch_summary.json` for `-n > 1`).

Notes:
- DACs are signed by the **bundled CHIP test PAI** (`chip-test-credentials.ts`)
  for the VID/PID — Matter forbids self-signed DACs. FFF2/8001 ships with the
  test PAI + CD; for other VID/PIDs pass `--pai-cert`/`--pai-key` (and `--cd`)
  or generation errors out.
- If Pyodide can't initialise (e.g. no network on first run), the `.bin` is
  skipped with a warning; all other artifacts still emit.
- Registration with the admin API is **not** performed — artifacts are emitted
  ready to register (DAC as `cert`, PAI as `ca_cert`).

## Tests

```bash
npm test            # vitest run (matter-gen only)
npm run test:watch
```

Golden vectors used (canonical Matter values):

- **SPAKE2+**: passcode `20202021`, salt `"SPAKE2P Key Salt"`, 1000 iters →
  the published connectedhomeip verifier.
- **QR**: discriminator `3840`, passcode `20202021`, Standard flow, on-network
  discovery → `MT:-24J0AFN00KA0648G00`.
- **Manual code**: same inputs → `34970112332`.
- **Certs**: PAA→PAI→DAC chain signature verification; PAI PEM round-trip
  through the DER parser.

## Integration notes (next phase)

- **PAI** — DACs are signed by the bundled CHIP test PAI for the VID/PID
  (`chip-test-credentials.ts`); FFF2/8001 is included. Add more VID/PIDs there,
  or pass `pai: { certPem, keyPem }` at call time. Self-signed DACs are not
  allowed by Matter, so generation errors when no PAI is available.
- **NVS packing** — feed `nvsRows` to `utils/nvs` (`generateNvsBin` /
  `generateNvsBinNode`): `namespace` rows
  open a namespace; `data`/`binary` values are `Uint8Array`, `u32` are numbers,
  `string`/`hex2bin` are strings.
- **Registration** — Matter nodes register via `POST /v1/admin/nodes` with
  `cert` = DAC and `ca_cert` = PAI (verify whether the bulk `registration-jobs`
  CSV path carries a per-row CA cert before reusing it).
