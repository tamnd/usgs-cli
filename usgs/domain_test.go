package usgs

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string
// functions and the host wiring, which need no network. The client's HTTP
// behaviour is covered in usgs_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "usgs" {
		t.Errorf("Scheme = %q, want usgs", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "usgs" {
		t.Errorf("Identity.Binary = %q, want usgs", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	typ, id, err := Domain{}.Classify("us7000m6al")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if typ != "event" || id != "us7000m6al" {
		t.Errorf("Classify = (%q, %q), want (event, us7000m6al)", typ, id)
	}
}

func TestLocate(t *testing.T) {
	got, err := Domain{}.Locate("event", "us7000m6al")
	want := "https://earthquake.usgs.gov/earthquakes/eventpage/us7000m6al"
	if err != nil || got != want {
		t.Errorf("Locate = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	// The domain is registered; verify scheme is served.
	domains := h.Domains()
	found := false
	for _, d := range domains {
		if d == "usgs" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("usgs scheme not registered, got %v", domains)
	}

	// ResolveOn turns a bare id into a URI under this domain.
	got, err := h.ResolveOn("usgs", "us7000m6al")
	if err != nil {
		t.Fatalf("ResolveOn: %v", err)
	}
	if got.String() != "usgs://event/us7000m6al" {
		t.Errorf("ResolveOn = %q, want usgs://event/us7000m6al", got.String())
	}
}
