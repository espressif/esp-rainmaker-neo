// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package certutil

import (
	"crypto/x509"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/collections"
	"time"
)

func VerifyCertificateChain(cert, caCert *x509.Certificate) error {
	// 1. Check Issuer matches CA Subject
	if !collections.StructsEqual(cert.Issuer, caCert.Subject) {
		return fmt.Errorf("issuer/subject mismatch")
	}

	// 2. Verify the certificate chain
	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	opts := x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: time.Now(),
	}

	if _, err := cert.Verify(opts); err != nil {
		return fmt.Errorf("chain verification failed: %v", err)
	}

	return nil
}
