package main

import "testing"

func TestGnomeAccel(t *testing.T) {
	for spec, accel := range map[string]string{
		"ctrl+alt+space": "<Control><Alt>space",
		"ctrl+super+f5":  "<Control><Super>F5",
		"shift+return":   "<Shift>Return",
	} {
		if got := gnomeAccel(spec); got != accel {
			t.Errorf("gnomeAccel(%q) = %q, want %q", spec, got, accel)
		}
		if got := specFromAccel(accel); got != spec {
			t.Errorf("specFromAccel(%q) = %q, want %q", accel, got, spec)
		}
	}
	if got := specFromAccel("<Primary><Super>space"); got != "ctrl+super+space" {
		t.Errorf("got %q", got)
	}
}
