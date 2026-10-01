package app_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/docmcp/docmcp/internal/app"
)

func TestVersionCommand(t *testing.T) {
	var out bytes.Buffer

	root := app.NewRootCommand("v0.1.0-test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("version command: %v", err)
	}

	if got := strings.TrimSpace(out.String()); got != "v0.1.0-test" {
		t.Fatalf("version output = %q, want %q", got, "v0.1.0-test")
	}
}

func TestRootCommand_ExposesVersionOnly(t *testing.T) {
	root := app.NewRootCommand("v0.1.0-test")

	for _, cmd := range root.Commands() {
		if cmd.Name() != "version" {
			t.Errorf("unexpected command %q in phase 0", cmd.Name())
		}
	}
}
