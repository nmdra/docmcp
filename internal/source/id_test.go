package source_test

import (
	"testing"

	"github.com/docmcp/docmcp/internal/source"
)

func TestNewLibraryID_Base(t *testing.T) {
	got, err := source.NewLibraryID("Pi", "")
	if err != nil {
		t.Fatalf("NewLibraryID: %v", err)
	}
	if want := "/local/pi"; got != want {
		t.Errorf("NewLibraryID(%q) = %q, want %q", "Pi", got, want)
	}
}

func TestNewLibraryID_Versioned(t *testing.T) {
	got, err := source.NewLibraryID("Pi", "0.99.2")
	if err != nil {
		t.Fatalf("NewLibraryID: %v", err)
	}
	if want := "/local/pi/0.99.2"; got != want {
		t.Errorf("NewLibraryID(%q, %q) = %q, want %q", "Pi", "0.99.2", got, want)
	}
}

func TestNewLibraryID_NormalizesName(t *testing.T) {
	cases := []struct{ name, version, want string }{
		{"Next.js", "", "/local/next-js"},
		{"Pi SDK", "", "/local/pi-sdk"},
		{"Pico CSS", "", "/local/pico-css"},
		{"Lightpanda", "0.4.1", "/local/lightpanda/0.4.1"},
		{"Company API", "v2", "/local/company-api/v2"},
		{"  pi  ", "", "/local/pi"},
		{"UPPER", "", "/local/upper"},
	}

	for _, tc := range cases {
		got, err := source.NewLibraryID(tc.name, tc.version)
		if err != nil {
			t.Errorf("NewLibraryID(%q, %q): %v", tc.name, tc.version, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NewLibraryID(%q, %q) = %q, want %q", tc.name, tc.version, got, tc.want)
		}
	}
}

func TestNewLibraryID_RejectsEmptyName(t *testing.T) {
	for _, name := range []string{"", "   "} {
		if _, err := source.NewLibraryID(name, "1.0"); err == nil {
			t.Errorf("NewLibraryID(%q) succeeded, want error", name)
		}
	}
}

func TestParseLibraryID_Base(t *testing.T) {
	id, err := source.ParseLibraryID("/local/pi")
	if err != nil {
		t.Fatalf("ParseLibraryID: %v", err)
	}
	if id.Namespace != "local" || id.Name != "pi" || id.Version != "" {
		t.Errorf("ParseLibraryID(/local/pi) = %+v, want {local pi \"\"}", id)
	}
}

func TestParseLibraryID_Version(t *testing.T) {
	id, err := source.ParseLibraryID("/local/pi/0.99.2")
	if err != nil {
		t.Fatalf("ParseLibraryID: %v", err)
	}
	if id.Namespace != "local" || id.Name != "pi" || id.Version != "0.99.2" {
		t.Errorf("ParseLibraryID(/local/pi/0.99.2) = %+v, want {local pi 0.99.2}", id)
	}
}

func TestParseLibraryID_RejectsMalformed(t *testing.T) {
	for _, raw := range []string{
		"", "pi", "/pi", "/local", "/local/pi/1/2", "/remote/pi", "/local/pi/",
	} {
		if _, err := source.ParseLibraryID(raw); err == nil {
			t.Errorf("ParseLibraryID(%q) succeeded, want error", raw)
		}
	}
}

func TestLibraryID_RoundTrip(t *testing.T) {
	raw, err := source.NewLibraryID("Next.js", "16")
	if err != nil {
		t.Fatalf("NewLibraryID: %v", err)
	}

	id, err := source.ParseLibraryID(raw)
	if err != nil {
		t.Fatalf("ParseLibraryID: %v", err)
	}

	again, err := id.String()
	if err != nil {
		t.Fatalf("String: %v", err)
	}
	if again != raw {
		t.Errorf("round trip = %q, want %q", again, raw)
	}
}

func TestLibraryID_IsDeterministic(t *testing.T) {
	first, err := source.NewLibraryID("Pi", "0.99.2")
	if err != nil {
		t.Fatalf("NewLibraryID: %v", err)
	}
	for i := range 50 {
		again, err := source.NewLibraryID("Pi", "0.99.2")
		if err != nil {
			t.Fatalf("NewLibraryID: %v", err)
		}
		if again != first {
			t.Fatalf("run %d = %q, want %q", i, again, first)
		}
	}
}
