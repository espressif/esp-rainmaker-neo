// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import "testing"

func TestColorTemperatureReportSkipsUninitializedValue(t *testing.T) {
	h := &ColorTemperatureControllerHandler{}

	props := ContextPropertyList{}
	if err := h.HandleReport(map[string]interface{}{"cct": 0}, "cct", nil, &props); err != nil {
		t.Fatalf("HandleReport(cct=0): %v", err)
	}
	if len(props) != 0 {
		t.Errorf("cct=0 must not be reported (Alexa schema requires >= 1000), got %v", props)
	}

	props = ContextPropertyList{}
	if err := h.HandleReport(map[string]interface{}{"cct": 4000}, "cct", nil, &props); err != nil {
		t.Fatalf("HandleReport(cct=4000): %v", err)
	}
	if len(props) != 1 {
		t.Errorf("cct=4000 must be reported, got %v", props)
	}
}

func TestStepColorTemperatureBounds(t *testing.T) {
	cases := []struct {
		current  int
		increase bool
		want     int
	}{
		{7000, true, 8000},                  // moves past the SetColorTemperature ceiling
		{2200, false, 1200},                 // moves below the SetColorTemperature floor
		{cctMaxKelvin, true, cctMaxKelvin},  // clamps at range max
		{cctMinKelvin, false, cctMinKelvin}, // clamps at range min
		{0, true, 5000},                     // unknown current falls back to mid-range
	}
	for _, tc := range cases {
		if got := stepColorTemperature(tc.current, tc.increase); got != tc.want {
			t.Errorf("stepColorTemperature(%d, %v) = %d, want %d", tc.current, tc.increase, got, tc.want)
		}
	}
}
