package source_collection

import (
	"log/slog"
	"os"
	"testing"
)

func TestNewClient(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c := NewClient(logger)
	if c == nil {
		t.Fatal("expected non-nil Client")
	}
}

func TestUpdateSettings(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	c := NewClient(logger).(*apiClient)

	s := EngineSettings{
		APIURL:   "https://custom.example.com",
		OCIHost:  "oci.example.com",
		Username: "user",
		Token:    "tok",
	}
	c.UpdateSettings(s)

	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.settings.APIURL != "https://custom.example.com" {
		t.Errorf("expected APIURL 'https://custom.example.com', got %q", c.settings.APIURL)
	}
	if c.settings.Username != "user" {
		t.Errorf("expected Username 'user', got %q", c.settings.Username)
	}
	if c.settings.Token != "tok" {
		t.Errorf("expected Token 'tok', got %q", c.settings.Token)
	}
}
