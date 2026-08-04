// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package group

import (
	"errors"
	"fmt"
)

// Matter TLV control byte constants
const (
	// TLV type masks
	TLVTypeMask    = 0x1F
	TLVTagTypeMask = 0xE0

	// TLV element types
	TLVTypeStructure = 0x15
	TLVTypeEndOfCont = 0x18
	TLVTypeByteStr1  = 0x10 // 1-byte length octet string
	TLVTypeByteStr2  = 0x11 // 2-byte length octet string

	// TLV tag types (in bits 5-7 of control byte)
	TLVTagAnonymous        = 0x00
	TLVTagContextSpecific1 = 0x20 // 1-byte context-specific tag
)

// Tag values within NOCSRElements structure
const (
	TagCSR             = 0x01
	TagCSRNonce        = 0x02
	TagVendorReserved1 = 0x03
	TagVendorReserved2 = 0x04
	TagVendorReserved3 = 0x05
)

// NOCSRElementsFields contains the parsed fields from a Matter NOCSRElements TLV structure
type NOCSRElementsFields struct {
	CSR             []byte // The Certificate Signing Request
	CSRNonce        []byte // The CSR nonce (challenge)
	VendorReserved1 []byte // Optional vendor reserved field 1 (contains nodeID for us)
	VendorReserved2 []byte // Optional vendor reserved field 2
	VendorReserved3 []byte // Optional vendor reserved field 3
}

// ParseNOCSRElements parses a Matter NOCSRElements TLV structure.
// The NOCSRElements structure has the following format:
// - Structure start (0x15)
// - CSR (tag 0x01): Octet string containing the DER-encoded CSR
// - CSRNonce (tag 0x02): Octet string containing the 32-byte nonce
// - VendorReserved1 (tag 0x03): Optional octet string
// - VendorReserved2 (tag 0x04): Optional octet string
// - VendorReserved3 (tag 0x05): Optional octet string
// - Structure end (0x18)
func ParseNOCSRElements(data []byte) (*NOCSRElementsFields, error) {
	if len(data) < 2 {
		return nil, errors.New("NOCSRElements data too short")
	}

	// First byte should be structure start (0x15)
	if data[0] != TLVTypeStructure {
		return nil, fmt.Errorf("expected structure start (0x15), got 0x%02X", data[0])
	}

	fields := &NOCSRElementsFields{}
	pos := 1 // Skip structure start

	for pos < len(data) {
		if pos >= len(data) {
			return nil, errors.New("unexpected end of TLV data")
		}

		controlByte := data[pos]
		pos++

		// Check for end of structure
		if controlByte == TLVTypeEndOfCont {
			break
		}

		// Parse tag type
		tagType := controlByte & TLVTagTypeMask
		elementType := controlByte & TLVTypeMask

		// We expect context-specific tags with 1-byte tag values
		if tagType != TLVTagContextSpecific1 {
			return nil, fmt.Errorf("expected context-specific tag, got 0x%02X", controlByte)
		}

		// Read the tag value
		if pos >= len(data) {
			return nil, errors.New("unexpected end of TLV data while reading tag")
		}
		tag := data[pos]
		pos++

		// Read the length based on element type
		var length int
		switch elementType {
		case TLVTypeByteStr1:
			// 1-byte length
			if pos >= len(data) {
				return nil, errors.New("unexpected end of TLV data while reading length")
			}
			length = int(data[pos])
			pos++
		case TLVTypeByteStr2:
			// 2-byte length (little-endian)
			if pos+1 >= len(data) {
				return nil, errors.New("unexpected end of TLV data while reading 2-byte length")
			}
			length = int(data[pos]) | (int(data[pos+1]) << 8)
			pos += 2
		default:
			return nil, fmt.Errorf("unsupported element type 0x%02X", elementType)
		}

		// Read the value
		if pos+length > len(data) {
			return nil, fmt.Errorf("TLV value extends beyond data: need %d bytes at pos %d, have %d", length, pos, len(data))
		}
		value := data[pos : pos+length]
		pos += length

		// Store the value based on tag
		switch tag {
		case TagCSR:
			fields.CSR = value
		case TagCSRNonce:
			fields.CSRNonce = value
		case TagVendorReserved1:
			fields.VendorReserved1 = value
		case TagVendorReserved2:
			fields.VendorReserved2 = value
		case TagVendorReserved3:
			fields.VendorReserved3 = value
		default:
			// Skip unknown tags
		}
	}

	// Validate required fields
	if fields.CSR == nil {
		return nil, errors.New("NOCSRElements missing required CSR field")
	}
	if fields.CSRNonce == nil {
		return nil, errors.New("NOCSRElements missing required CSRNonce field")
	}

	return fields, nil
}

// ExtractCSRFromNOCSRElements extracts just the CSR from NOCSRElements for convenience.
// This returns the raw DER-encoded CSR that can be wrapped in PEM format.
func ExtractCSRFromNOCSRElements(data []byte) ([]byte, error) {
	fields, err := ParseNOCSRElements(data)
	if err != nil {
		return nil, err
	}
	return fields.CSR, nil
}
