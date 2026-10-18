package main

import "testing"

func TestIsPlaceholderName(t *testing.T) {
	for name, want := range map[string]bool{
		"":                   true,
		"  ":                 true,
		"123456789012345":    true,
		"+31612345678":       true,
		"Jane Doe":           false,
		"Sunny😁":             false,
		"Group 120363012345": false,
	} {
		if got := isPlaceholderName(name); got != want {
			t.Errorf("isPlaceholderName(%q) = %v, want %v", name, got, want)
		}
	}
}
