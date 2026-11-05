package profile

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestLiveSubscriptionCompatibility(t *testing.T) {
	source := os.Getenv("MARSHRUT_TEST_SUBSCRIPTION")
	if source == "" {
		t.Skip("personal subscription provided only through the test environment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", source, nil)
	if e != nil {
		t.Fatal("invalid test source")
	}
	req.Header.Set("User-Agent", "Marshrut/0.1 sing-box")
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 25 * time.Second}
	response, e := client.Do(req)
	if e != nil {
		t.Fatal("subscription transport unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("subscription HTTP %d", response.StatusCode)
	}
	body, e := io.ReadAll(io.LimitReader(response.Body, MaxBytes+1))
	if e != nil {
		t.Fatal("subscription could not be read")
	}
	result, e := Decode(string(body))
	clear(body)
	if e != nil {
		t.Fatalf("subscription not supported: %s", e)
	}
	if len(result.Warnings) > 0 {
		t.Fatalf("subscription contains %d rejected entries; no credentials logged", len(result.Warnings))
	}
	t.Logf("Imported %d live VLESS profiles without dropping required parameters", len(result.Servers))
}
