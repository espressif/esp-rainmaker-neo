// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package group

import (
	"bytes"
	"testing"
)

// buildNOCSRElementsTLV is a helper function that builds a valid NOCSRElements TLV structure
func buildNOCSRElementsTLV(csr, csrNonce, vendorReserved1 []byte) []byte {
	var buf bytes.Buffer

	// Structure start
	buf.WriteByte(TLVTypeStructure)

	// CSR (tag 0x01)
	if csr != nil {
		buf.WriteByte(TLVTagContextSpecific1 | TLVTypeByteStr2) // Context-specific tag + 2-byte length octet string
		buf.WriteByte(TagCSR)
		buf.WriteByte(byte(len(csr) & 0xFF))        // Length low byte
		buf.WriteByte(byte((len(csr) >> 8) & 0xFF)) // Length high byte
		buf.Write(csr)
	}

	// CSRNonce (tag 0x02)
	if csrNonce != nil {
		buf.WriteByte(TLVTagContextSpecific1 | TLVTypeByteStr1) // Context-specific tag + 1-byte length octet string
		buf.WriteByte(TagCSRNonce)
		buf.WriteByte(byte(len(csrNonce)))
		buf.Write(csrNonce)
	}

	// VendorReserved1 (tag 0x03)
	if vendorReserved1 != nil {
		buf.WriteByte(TLVTagContextSpecific1 | TLVTypeByteStr1) // Context-specific tag + 1-byte length octet string
		buf.WriteByte(TagVendorReserved1)
		buf.WriteByte(byte(len(vendorReserved1)))
		buf.Write(vendorReserved1)
	}

	// Structure end
	buf.WriteByte(TLVTypeEndOfCont)

	return buf.Bytes()
}

func TestParseNOCSRElements_ValidInput(t *testing.T) {
	// Sample CSR data (just some bytes for testing)
	testCSR := []byte{0x30, 0x82, 0x01, 0x00, 0x02, 0x01, 0x00} // Partial DER-encoded CSR header
	testNonce := make([]byte, 32)
	for i := range testNonce {
		testNonce[i] = byte(i)
	}
	testNodeID := []byte("TestNodeID123")

	tlvData := buildNOCSRElementsTLV(testCSR, testNonce, testNodeID)

	fields, err := ParseNOCSRElements(tlvData)
	if err != nil {
		t.Fatalf("ParseNOCSRElements failed: %v", err)
	}

	if !bytes.Equal(fields.CSR, testCSR) {
		t.Errorf("CSR mismatch: expected %v, got %v", testCSR, fields.CSR)
	}

	if !bytes.Equal(fields.CSRNonce, testNonce) {
		t.Errorf("CSRNonce mismatch: expected %v, got %v", testNonce, fields.CSRNonce)
	}

	if !bytes.Equal(fields.VendorReserved1, testNodeID) {
		t.Errorf("VendorReserved1 mismatch: expected %v, got %v", testNodeID, fields.VendorReserved1)
	}
}

func TestParseNOCSRElements_WithoutVendorReserved(t *testing.T) {
	testCSR := []byte{0x30, 0x82, 0x01, 0x00}
	testNonce := make([]byte, 32)
	for i := range testNonce {
		testNonce[i] = byte(i + 10)
	}

	tlvData := buildNOCSRElementsTLV(testCSR, testNonce, nil)

	fields, err := ParseNOCSRElements(tlvData)
	if err != nil {
		t.Fatalf("ParseNOCSRElements failed: %v", err)
	}

	if !bytes.Equal(fields.CSR, testCSR) {
		t.Errorf("CSR mismatch: expected %v, got %v", testCSR, fields.CSR)
	}

	if !bytes.Equal(fields.CSRNonce, testNonce) {
		t.Errorf("CSRNonce mismatch: expected %v, got %v", testNonce, fields.CSRNonce)
	}

	if fields.VendorReserved1 != nil {
		t.Errorf("VendorReserved1 should be nil, got %v", fields.VendorReserved1)
	}
}

func TestParseNOCSRElements_MissingCSR(t *testing.T) {
	// Build TLV without CSR (only nonce)
	var buf bytes.Buffer
	buf.WriteByte(TLVTypeStructure)

	// CSRNonce only (tag 0x02)
	testNonce := make([]byte, 32)
	buf.WriteByte(TLVTagContextSpecific1 | TLVTypeByteStr1)
	buf.WriteByte(TagCSRNonce)
	buf.WriteByte(byte(len(testNonce)))
	buf.Write(testNonce)

	buf.WriteByte(TLVTypeEndOfCont)

	_, err := ParseNOCSRElements(buf.Bytes())
	if err == nil {
		t.Error("Expected error for missing CSR, got nil")
	}
}

func TestParseNOCSRElements_MissingNonce(t *testing.T) {
	// Build TLV without nonce (only CSR)
	var buf bytes.Buffer
	buf.WriteByte(TLVTypeStructure)

	// CSR only (tag 0x01)
	testCSR := []byte{0x30, 0x82, 0x01, 0x00}
	buf.WriteByte(TLVTagContextSpecific1 | TLVTypeByteStr1)
	buf.WriteByte(TagCSR)
	buf.WriteByte(byte(len(testCSR)))
	buf.Write(testCSR)

	buf.WriteByte(TLVTypeEndOfCont)

	_, err := ParseNOCSRElements(buf.Bytes())
	if err == nil {
		t.Error("Expected error for missing nonce, got nil")
	}
}

func TestParseNOCSRElements_InvalidStructureStart(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02} // Invalid start byte

	_, err := ParseNOCSRElements(data)
	if err == nil {
		t.Error("Expected error for invalid structure start, got nil")
	}
}

func TestParseNOCSRElements_TooShort(t *testing.T) {
	data := []byte{0x15} // Only structure start, no end

	_, err := ParseNOCSRElements(data)
	if err == nil {
		t.Error("Expected error for data too short, got nil")
	}
}

func TestExtractCSRFromNOCSRElements(t *testing.T) {
	testCSR := []byte{0x30, 0x82, 0x01, 0x00, 0x02, 0x01, 0x00}
	testNonce := make([]byte, 32)

	tlvData := buildNOCSRElementsTLV(testCSR, testNonce, nil)

	csr, err := ExtractCSRFromNOCSRElements(tlvData)
	if err != nil {
		t.Fatalf("ExtractCSRFromNOCSRElements failed: %v", err)
	}

	if !bytes.Equal(csr, testCSR) {
		t.Errorf("CSR mismatch: expected %v, got %v", testCSR, csr)
	}
}
