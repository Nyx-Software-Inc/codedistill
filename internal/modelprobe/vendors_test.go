package modelprobe

import (
	"strings"
	"testing"
)

// Any vendor listed as unsupported must carry a way forward. Silently omitting
// one makes the product look like it does not do cloud models; listing it as if
// it worked produces a confident failure at the first call.
func TestUnsupportedVendorsSayWhatToDoInstead(t *testing.T) {
	var unsupported []string
	for _, v := range KnownVendors {
		if !v.Supported {
			unsupported = append(unsupported, v.ID)
		}
	}
	for _, id := range unsupported {
		v, ok := VendorByID(id)
		if !ok {
			t.Fatalf("%s is not listed at all", id)
		}
		if v.Supported {
			t.Errorf("%s claims support; nothing here speaks its protocol", id)
		}
		if v.Note == "" {
			t.Errorf("%s is unsupported with no explanation or workaround", id)
		}
		if v.Protocol != "" {
			t.Errorf("%s names a protocol it cannot actually use: %q", id, v.Protocol)
		}
	}
}

// Every supported vendor must carry enough to build a working connection.
func TestSupportedVendorsAreComplete(t *testing.T) {
	for _, v := range KnownVendors {
		if !v.Supported {
			continue
		}
		if v.Protocol == "" {
			t.Errorf("%s is supported but names no protocol", v.ID)
		}
		// Kept in step with the adapters in internal/ollama by hand, on
		// purpose: marking a vendor supported is the claim that something can
		// actually call it, and this is the line that makes adding a vendor
		// without an adapter fail here rather than at a user's first request.
		switch v.Protocol {
		case "ollama", "openai", "anthropic", "gemini", "bedrock", "azure":
		default:
			t.Errorf("%s uses protocol %q, which no adapter implements", v.ID, v.Protocol)
		}
		// "custom" deliberately has no default: the whole point is that the
		// user supplies one.
		if v.DefaultEndpoint == "" && v.ID != "custom" {
			t.Errorf("%s has no default endpoint, so the field starts empty", v.ID)
		}
	}
}

// The two vendors Rich actually uses must be reachable, and each must declare
// what it can and cannot prove — Gemini publishes input limits, Anthropic does
// not, and the UI is allowed to claim only what is true.
func TestAnthropicAndGeminiAreSupported(t *testing.T) {
	a, _ := VendorByID("anthropic")
	if !a.Supported || a.Protocol != "anthropic" {
		t.Errorf("anthropic: supported=%v protocol=%q", a.Supported, a.Protocol)
	}
	if a.ReportsContext {
		t.Error("Anthropic does not publish context windows; claiming it does would let a typed number look verified")
	}
	if !strings.Contains(a.Note, "tool") {
		t.Error("the note does not mention that JSON is forced with a tool, which is the non-obvious part")
	}

	g, _ := VendorByID("gemini")
	if !g.Supported || g.Protocol != "gemini" {
		t.Errorf("gemini: supported=%v protocol=%q", g.Supported, g.Protocol)
	}
	if !g.ReportsContext {
		t.Error("Gemini publishes inputTokenLimit; not using it wastes the one cloud vendor that can be verified")
	}
}

// Claiming a vendor reports context windows when it does not would let the UI
// present a typed number as verified — the exact failure this package exists
// to prevent.
func TestOpenAIIsNotClaimedToReportContext(t *testing.T) {
	v, _ := VendorByID("openai")
	if v.ReportsContext {
		t.Error("OpenAI's /models returns ids and owners, not context windows")
	}
	if v.Note == "" {
		t.Error("no note explaining that the context figure is user-supplied")
	}
}
