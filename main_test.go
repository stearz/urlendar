package main

import "testing"

func TestConfigFromEnvironment(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("PUBLIC_ORIGIN", "https://calendar.example")

	config := configFromEnvironment()
	if config.Address != ":9090" {
		t.Fatalf("address = %q", config.Address)
	}
	if config.PublicOrigin != "https://calendar.example" {
		t.Fatalf("origin = %q", config.PublicOrigin)
	}
}

func TestConfigDefaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("PUBLIC_ORIGIN", "")

	config := configFromEnvironment()
	if config.Address != ":8080" {
		t.Fatalf("address = %q", config.Address)
	}
	if config.PublicOrigin != "https://urlendar.stearz.net" {
		t.Fatalf("origin = %q", config.PublicOrigin)
	}
}
