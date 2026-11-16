package prefill

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestPublic(t *testing.T) {
	t.Parallel()
	for addr, want := range map[string]bool{
		"93.184.215.14": true, "2606:4700::1": true,
		"127.0.0.1": false, "::1": false, "::ffff:127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false,
		"192.168.1.1": false, "169.254.169.254": false, "fe80::1": false, "fd00::1": false, "100.64.0.1": false,
		"0.0.0.0": false, "0.1.2.3": false, "224.0.0.1": false, "255.255.255.255": false, "198.18.0.1": false,
		"64:ff9b::7f00:1": false, "2002:7f00:1::": false, "2001:0:7f00:1::": false, "::": false,
		"::7f00:1": false, "::a00:1": false, "::ffff:0:7f00:1": false, "fec0::1": false, "100::1": false,
		"2001:10::1": false, "2001:20::1": false, "5f00::1": false, "3fff::1": false,
	} {
		if got := public(netip.MustParseAddr(addr)); got != want {
			t.Errorf("public(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestDialControl(t *testing.T) {
	t.Parallel()
	for addr, ok := range map[string]bool{
		"93.184.215.14:443": true, "[2606:4700::1]:80": true,
		"93.184.215.14:22": false, "93.184.215.14:8080": false, "127.0.0.1:443": false, "[::ffff:10.0.0.1]:80": false,
	} {
		if err := dialControl("tcp", addr, nil); (err == nil) != ok {
			t.Errorf("dialControl(%s) = %v, want ok %v", addr, err, ok)
		}
	}
}

func TestCheckRedirect(t *testing.T) {
	t.Parallel()
	req := func(u string) *http.Request { pu, _ := url.Parse(u); return &http.Request{URL: pu} }
	via := []*http.Request{req("https://a.example")}
	if err := checkRedirect(req("https://b.example"), via); err != nil {
		t.Errorf("one https redirect: %v", err)
	}
	if err := checkRedirect(req("ftp://b.example"), via); err == nil {
		t.Error("redirect to ftp accepted")
	}
	if err := checkRedirect(req("https://b.example"), append(via, via[0], via[0])); err == nil {
		t.Error("fourth redirect accepted")
	}
}

// TestNewClientRefusesLoopback: the real client cannot reach a local server, even by its literal address.
func TestNewClientRefusesLoopback(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("secret")) }))
	t.Cleanup(srv.Close)
	_, err := Fetch(context.Background(), NewClient(), srv.URL+"/g/SECRET?token=T", now)
	if err == nil || !strings.Contains(err.Error(), "refusing to dial") {
		t.Fatalf("Fetch(%s) err = %v, want a refused dial", srv.URL, err)
	}
	// The error goes to logs: the host only, never the path or query.
	if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "token") {
		t.Errorf("error leaks the URL: %v", err)
	}
}

func TestFetch(t *testing.T) {
	t.Parallel()
	dice, err := os.ReadFile("testdata/dice.html")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" {
			t.Errorf("request carries Cookie or Referer: %v", r.Header)
		}
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(dice)
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(dice)
		default:
			http.Error(w, "blocked", http.StatusForbidden)
		}
	}))
	t.Cleanup(srv.Close)
	// A plain client: the address checks are TestNewClientRefusesLoopback's job.
	res, err := Fetch(context.Background(), srv.Client(), srv.URL+"/page", now)
	if err != nil || res.Artist != "Les Hiboux" || res.Date != "2026-11-27" {
		t.Fatalf("Fetch = %+v, %v", res, err)
	}
	for _, path := range []string{"/json", "/challenge"} {
		if _, err := Fetch(context.Background(), srv.Client(), srv.URL+path, now); err == nil {
			t.Errorf("Fetch(%s): want an error", path)
		}
	}
}
