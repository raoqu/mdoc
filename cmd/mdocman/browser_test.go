package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenBrowserActivatesOrFallsBack(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS browser launcher")
	}
	for _, test := range []struct {
		name       string
		activation string
		opener     string
		wantOpen   bool
		wantError  bool
	}{
		{"existing tab", "printf activated", "exit 0", false, false},
		{"no tab", "printf not-found", "exit 0", true, false},
		{"automation denied", "exit 1", "exit 0", true, false},
		{"opener failed", "printf not-found", "exit 1", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			bin := t.TempDir()
			activationArgs := filepath.Join(bin, "activation-args")
			opened := filepath.Join(bin, "opened")
			scripts := map[string]string{
				"osascript": "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$MDOC_TEST_ACTIVATION_ARGS\"\n" + test.activation + "\n",
				"open":      "#!/bin/sh\nprintf '%s' \"$1\" > \"$MDOC_TEST_OPENED\"\n" + test.opener + "\n",
			}
			for name, content := range scripts {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(content), 0700); err != nil {
					t.Fatal(err)
				}
			}
			// Never contact a real browser during tests.
			t.Setenv("PATH", bin)
			t.Setenv("MDOC_TEST_ACTIVATION_ARGS", activationArgs)
			t.Setenv("MDOC_TEST_OPENED", opened)
			target := "http://localhost:2026/?doc=working#cursor"
			err := openBrowser(target)
			if (err != nil) != test.wantError {
				t.Fatalf("openBrowser = %v", err)
			}
			args, err := os.ReadFile(activationArgs)
			if err != nil || !strings.HasSuffix(string(args), "\n"+target+"\n") {
				t.Fatalf("activation arguments = %q, %v", args, err)
			}
			actual, err := os.ReadFile(opened)
			if test.wantOpen {
				if err != nil || string(actual) != target {
					t.Fatalf("OS opener = %q, %v", actual, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("created another tab instead of activating: %q, %v", actual, err)
			}
		})
	}
}
