package auth

import (
	"net/url"
	"testing"
)

func TestLoginURLEncodesRedirectAndState(t *testing.T) {
	got, err := loginURL("https://api.example/base", "http://localhost:12345/callback?next=a&b=c", "state/value+with=chars")
	if err != nil {
		t.Fatalf("loginURL failed: %v", err)
	}

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse login URL: %v", err)
	}
	if parsed.String() != got {
		t.Fatalf("URL was not canonical: %q", got)
	}
	if parsed.Path != "/base/login" {
		t.Fatalf("unexpected path %q", parsed.Path)
	}
	values := parsed.Query()
	if values.Get("redirect_uri") != "http://localhost:12345/callback?next=a&b=c" {
		t.Fatalf("redirect_uri was not preserved: %q", values.Get("redirect_uri"))
	}
	if values.Get("state") != "state/value+with=chars" {
		t.Fatalf("state was not preserved: %q", values.Get("state"))
	}
}

func TestLoginURLRejectsUnsupportedSchemes(t *testing.T) {
	if _, err := loginURL("file:///tmp/api", "http://localhost/callback", "state"); err == nil {
		t.Fatal("expected unsupported scheme error")
	}
}
