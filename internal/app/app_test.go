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

func TestRootCommand_ExposesTheAgreedCommands(t *testing.T) {
	root := app.NewRootCommand("v0.1.0-test")

	want := map[string]bool{
		"add": true, "list": true, "info": true,
		"sync": true, "remove": true, "version": true,
	}

	got := map[string]bool{}
	for _, cmd := range root.Commands() {
		got[cmd.Name()] = true
	}

	for name := range want {
		if !got[name] {
			t.Errorf("missing command %q", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("unexpected command %q", name)
		}
	}
}
