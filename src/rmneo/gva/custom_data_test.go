// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import "testing"

// customData is supplied by the caller, so an absent, empty or non-string groupID must be
// rejected: the execute and query paths use it to load the node permissions that authorize
// the request, and skipping that load would leave the request unauthorized.
func TestGroupIDFromCustomData(t *testing.T) {
	tests := []struct {
		name       string
		customData map[string]interface{}
		want       string
		wantErr    bool
	}{
		{name: "valid", customData: map[string]interface{}{"groupID": "abc123"}, want: "abc123"},
		{name: "missing key", customData: map[string]interface{}{}, wantErr: true},
		{name: "nil map", customData: nil, wantErr: true},
		{name: "empty value", customData: map[string]interface{}{"groupID": ""}, wantErr: true},
		{name: "non-string value", customData: map[string]interface{}{"groupID": 42}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := groupIDFromCustomData(tt.customData)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got groupID %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("groupIDFromCustomData() = %q, want %q", got, tt.want)
			}
		})
	}
}
