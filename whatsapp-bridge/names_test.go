package main

import "testing"

func TestIsPlaceholderName(t *testing.T) {
	for name, want := range map[string]bool{
		"":                   true,
		"  ":                 true,
		"112588289536078":    true,
		"+31612345678":       true,
		"Ria Meulendijks":    false,
		"Tommy😁":             false,
		"Group 120363012345": false,
	} {
		if got := isPlaceholderName(name); got != want {
			t.Errorf("isPlaceholderName(%q) = %v, want %v", name, got, want)
		}
	}
}
