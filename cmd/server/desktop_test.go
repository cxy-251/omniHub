package main

import "testing"

func TestDesktopOptionsKeepStartupExplicit(t *testing.T) {
	options, err := parseLaunchOptions([]string{"--desktop", "--client-path", "/a path/client"})
	if err != nil || !options.desktop || options.clientPath != "/a path/client" {
		t.Fatalf("valid startup: %+v %v", options, err)
	}
	for _, args := range [][]string{{"--client-path", "arbitrary"}, {"unexpected"}, {"--unknown"}} {
		if _, err := parseLaunchOptions(args); err == nil {
			t.Fatalf("unexpected startup arguments accepted: %v", args)
		}
	}
	if err := startDesktop("relative-client"); err == nil {
		t.Fatal("relative native executable accepted")
	}
}
