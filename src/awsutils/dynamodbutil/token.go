// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil

import (
	"encoding/base64"
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// tokenAttr is one key attribute with its type tag. The format must not change: clients hold tokens across deploys.
type tokenAttr struct {
	Value string `json:"value"`
	Type  string `json:"type"`
}

// EncodeToken turns a key into a URL-safe pagination token: {"id": S "a", "ts": N "5"} -> base64url of {"id":{"value":"a","type":"S"},"ts":{"value":"5","type":"N"}}. Attributes of other types are dropped, as key attributes are only S, N or B.
func EncodeToken(key Key) (string, error) {
	encoded := make(map[string]tokenAttr, len(key))
	for k, v := range key {
		switch av := v.(type) {
		case *types.AttributeValueMemberS:
			encoded[k] = tokenAttr{Value: av.Value, Type: "S"}
		case *types.AttributeValueMemberN:
			encoded[k] = tokenAttr{Value: av.Value, Type: "N"}
		case *types.AttributeValueMemberB:
			encoded[k] = tokenAttr{Value: base64.RawURLEncoding.EncodeToString(av.Value), Type: "B"}
		}
	}
	b, err := json.Marshal(encoded)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DecodeToken is the inverse of EncodeToken. An unknown type tag decodes as a string, so tokens already issued to clients keep working.
func DecodeToken(token string) (Key, error) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, err
	}
	var decoded map[string]tokenAttr
	if err := json.Unmarshal(b, &decoded); err != nil {
		return nil, err
	}
	key := make(Key, len(decoded))
	for k, v := range decoded {
		switch v.Type {
		case "N":
			key[k] = &types.AttributeValueMemberN{Value: v.Value}
		case "B":
			raw, err := base64.RawURLEncoding.DecodeString(v.Value)
			if err != nil {
				return nil, err
			}
			key[k] = &types.AttributeValueMemberB{Value: raw}
		default:
			key[k] = &types.AttributeValueMemberS{Value: v.Value}
		}
	}
	return key, nil
}
