/**
 * Bundled CHIP **test** attestation credentials (PAI + CD), keyed by VID/PID.
 *
 * Matter does not allow self-signed DACs — every DAC must chain to a PAA via a
 * PAI. For evaluation we sign DACs with the official connectedhomeip *test* PAI
 * (which chains to the well-known Matter Test PAA that test commissioners
 * trust), exactly as esp-matter-mfg-tool / factory_autoreg do.
 *
 * Source: esp-matter/connectedhomeip/connectedhomeip/credentials/test/
 *   attestation/Chip-Test-PAI-FFF2-8001-{Cert,Key}.pem
 *   certification-declaration/Chip-Test-CD-FFF2-8001.der
 *
 * THESE ARE TEST CREDENTIALS — for evaluation/commissioning against test
 * controllers only, never for production devices.
 */

const PAI_FFF2_8001_CERT_PEM = `-----BEGIN CERTIFICATE-----
MIIBvTCCAWSgAwIBAgIIZTqIfBv+Fi4wCgYIKoZIzj0EAwIwGjEYMBYGA1UEAwwP
TWF0dGVyIFRlc3QgUEFBMCAXDTIxMDYyODE0MjM0M1oYDzk5OTkxMjMxMjM1OTU5
WjBGMRgwFgYDVQQDDA9NYXR0ZXIgVGVzdCBQQUkxFDASBgorBgEEAYKifAIBDARG
RkYyMRQwEgYKKwYBBAGConwCAgwEODAwMTBZMBMGByqGSM49AgEGCCqGSM49AwEH
A0IABCwGPCCLt88/idiccLJo3sLwrYkZLwIvlUetzHIqBoBpynI1YIO3JHcbIXZM
skxXEbU+/of+T+C0cxQbzKEEso2jZjBkMBIGA1UdEwEB/wQIMAYBAf8CAQAwDgYD
VR0PAQH/BAQDAgEGMB0GA1UdDgQWBBTQWptncaGjepvBnZXotduPQwC2OjAfBgNV
HSMEGDAWgBR4XOcFuGuPTm/Hk6pgy0PqaWiC1TAKBggqhkjOPQQDAgNHADBEAiBg
XpfcZeDFc/n49qW5aBzbm2vuHp/9WJZzqgPTYV79YAIgJuiQtx4fnULnk6SOzNvI
+AgYB/L7Nwo9JJevN9xKpTM=
-----END CERTIFICATE-----
`

const PAI_FFF2_8001_KEY_PEM = `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIOxW/BFZusWpALRTftT6DtRUo/1F6v7Gw/ZfYY96LhrhoAoGCCqGSM49
AwEHoUQDQgAELAY8IIu3zz+J2JxwsmjewvCtiRkvAi+VR63McioGgGnKcjVgg7ck
dxshdkyyTFcRtT7+h/5P4LRzFBvMoQSyjQ==
-----END EC PRIVATE KEY-----
`

// Chip-Test-CD-FFF2-8001.der (Certification Declaration), base64.
const CD_FFF2_8001_B64 =
  'MIHoBgkqhkiG9w0BBwKggdowgdcCAQMxDTALBglghkgBZQMEAgEwRQYJKoZIhvcNAQcBoDgENhUkAAElAfL/NgIFAYAYJQM0EiwEE1pJRzIwMTQxWkIzMzAwMDEtMjQkBQAkBgAlB3aYJAgAGDF8MHoCAQOAFGL6gjNZrPqplj4c+hQK3fUE83FgMAsGCWCGSAFlAwQCATAKBggqhkjOPQQDAgRGMEQCIAktcB3B8/0pWVrlt/JaxLmGF7pDRPBKrOulEPAwJrolAiBbQNFL7/ALGmRmKyCk7wX7AR28mmD4tlC9aXgsY/z2hA=='

function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) {out[i] = bin.charCodeAt(i)}
  return out
}

export interface ChipTestCredentials {
  paiCertPem: string
  paiKeyPem: string
  /** Certification Declaration (DER) for the `cert-dclrn` NVS entry. */
  cdDer: Uint8Array
}

const credentialsKey = (vendorId: number, productId: number): string =>
  `0x${(vendorId & 0xffff).toString(16)}/0x${(productId & 0xffff).toString(16)}`

const BUNDLE: Record<string, () => ChipTestCredentials> = {
  [credentialsKey(0xfff2, 0x8001)]: () => ({
    paiCertPem: PAI_FFF2_8001_CERT_PEM,
    paiKeyPem: PAI_FFF2_8001_KEY_PEM,
    cdDer: base64ToBytes(CD_FFF2_8001_B64),
  }),
}

/** VID/PID combinations with bundled CHIP test credentials. */
export const BUNDLED_TEST_CREDENTIAL_KEYS = Object.keys(BUNDLE)

/** Return bundled CHIP test PAI + CD for a VID/PID, or undefined if not bundled. */
export function getChipTestCredentials(
  vendorId: number,
  productId: number,
): ChipTestCredentials | undefined {
  return BUNDLE[credentialsKey(vendorId, productId)]?.()
}
