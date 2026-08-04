// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"encoding/base64"
	"encoding/json"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// startKeyAttrValue holds a DynamoDB attribute value with its type tag for
// JSON serialization. Supported types: "S", "N", "B", "BOOL".
type startKeyAttrValue struct {
	Value string `json:"value"`
	Type  string `json:"type"`
}

// EncodePaginationToken encodes a DynamoDB last-evaluated-key into a URL-safe
// base64 string suitable for use as a pagination token in API query parameters.
//
// Example:
//
//	Input (DynamoDB LastEvaluatedKey):
//	  map[string]types.AttributeValue{
//	    "node_key_dt": &types.AttributeValueMemberS{Value: "abc.temperature.float"},
//	    "ts":            &types.AttributeValueMemberN{Value: "1640995209"},
//	  }
//
//	Intermediate JSON (before base64):
//	  {"node_key_dt":{"value":"abc.temperature.float","type":"S"},
//	   "ts":{"value":"1640995209","type":"N"}}
//
//	Output (URL-safe base64, no padding):
//	  "eyJub2RlX3BhcmFtX2R0Ijp7InZhbHVlIjoiYWJjLnRlbXBlcmF0dXJlLmZsb2F0IiwidHlwZSI6IlMifSwidHMiOnsidmFsdWUiOiIxNjQwOTk1MjA5IiwidHlwZSI6Ik4ifX0"
func EncodePaginationToken(data map[string]types.AttributeValue) (string, error) {
	encoded := make(map[string]startKeyAttrValue)
	for k, v := range data {
		switch av := v.(type) {
		case *types.AttributeValueMemberS:
			encoded[k] = startKeyAttrValue{Value: av.Value, Type: "S"}
		case *types.AttributeValueMemberN:
			encoded[k] = startKeyAttrValue{Value: av.Value, Type: "N"}
		case *types.AttributeValueMemberB:
			encoded[k] = startKeyAttrValue{Value: base64.RawURLEncoding.EncodeToString(av.Value), Type: "B"}
		case *types.AttributeValueMemberBOOL:
			encoded[k] = startKeyAttrValue{Value: strconv.FormatBool(av.Value), Type: "BOOL"}
		}
	}
	b, err := json.Marshal(encoded)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DecodePaginationToken decodes a URL-safe base64 pagination token back into
// a DynamoDB exclusive-start-key map. It is the inverse of EncodePaginationToken.
//
// Example:
//
//	Input (URL-safe base64 token):
//	  "eyJub2RlX3BhcmFtX2R0Ijp7InZhbHVlIjoiYWJjLnRlbXBlcmF0dXJlLmZsb2F0IiwidHlwZSI6IlMifSwidHMiOnsidmFsdWUiOiIxNjQwOTk1MjA5IiwidHlwZSI6Ik4ifX0"
//
//	Output (DynamoDB ExclusiveStartKey):
//	  map[string]types.AttributeValue{
//	    "node_key_dt": &types.AttributeValueMemberS{Value: "abc.temperature.float"},
//	    "ts":            &types.AttributeValueMemberN{Value: "1640995209"},
//	  }
func DecodePaginationToken(token string) (map[string]types.AttributeValue, error) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, err
	}
	var decoded map[string]startKeyAttrValue
	if err := json.Unmarshal(b, &decoded); err != nil {
		return nil, err
	}
	result := make(map[string]types.AttributeValue)
	for k, v := range decoded {
		switch v.Type {
		case "N":
			result[k] = &types.AttributeValueMemberN{Value: v.Value}
		case "B":
			raw, err := base64.RawURLEncoding.DecodeString(v.Value)
			if err != nil {
				return nil, err
			}
			result[k] = &types.AttributeValueMemberB{Value: raw}
		case "BOOL":
			boolVal, err := strconv.ParseBool(v.Value)
			if err != nil {
				return nil, err
			}
			result[k] = &types.AttributeValueMemberBOOL{Value: boolVal}
		default:
			result[k] = &types.AttributeValueMemberS{Value: v.Value}
		}
	}
	return result, nil
}
