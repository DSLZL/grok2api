package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeAppConfigSeed(t *testing.T, configPath string, publicEnabled bool) {
	t.Helper()
	content := []byte("[app]\npublic_enabled = " + map[bool]string{true: "true", false: "false"}[publicEnabled] + "\n")
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatalf("WriteFile(config.toml) error = %v", err)
	}
}

func TestNewApplicationAddress(t *testing.T) {
	application, err := NewApplication(ServerConfig{
		Host:    "127.0.0.1",
		Port:    18080,
		Workers: 1,
	})
	if err != nil {
		t.Fatalf("NewApplication() error = %v", err)
	}

	if application.Address() != "127.0.0.1:18080" {
		t.Fatalf("Address() = %q, want %q", application.Address(), "127.0.0.1:18080")
	}
}

func TestNewApplicationHealthz(t *testing.T) {
	application, err := NewApplication(ServerConfig{
		Host:    "127.0.0.1",
		Port:    18080,
		Workers: 1,
	})
	if err != nil {
		t.Fatalf("NewApplication() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()

	application.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if rr.Body.String() != "ok" {
		t.Fatalf("body = %q, want %q", rr.Body.String(), "ok")
	}
}

func TestNewApplicationRegistersPagesWithConfigManager(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	projectRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("Chdir(project root) error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	tempDataDir := t.TempDir()
	tempConfigPath := filepath.Join(tempDataDir, "config.toml")

	t.Run("public enabled root redirects to /login", func(t *testing.T) {
		writeAppConfigSeed(t, tempConfigPath, true)
		application, err := NewApplication(ServerConfig{
			Host:       "127.0.0.1",
			Port:       18080,
			Workers:    1,
			ConfigPath: tempConfigPath,
		})
		if err != nil {
			t.Fatalf("NewApplication() error = %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		application.Handler().ServeHTTP(rr, req)

		if rr.Code != http.StatusTemporaryRedirect {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusTemporaryRedirect)
		}
		if got := rr.Header().Get("Location"); got != "/login" {
			t.Fatalf("Location = %q, want %q", got, "/login")
		}
	})

	t.Run("public disabled admin login page is served", func(t *testing.T) {
		writeAppConfigSeed(t, tempConfigPath, false)
		application, err := NewApplication(ServerConfig{
			Host:       "127.0.0.1",
			Port:       18080,
			Workers:    1,
			ConfigPath: tempConfigPath,
		})
		if err != nil {
			t.Fatalf("NewApplication() error = %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
		rr := httptest.NewRecorder()
		application.Handler().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
		}
	})
}

func TestNewApplicationWithoutConfigManagerReturns404ForPages(t *testing.T) {
	application, err := NewApplication(ServerConfig{
		Host:    "127.0.0.1",
		Port:    18080,
		Workers: 1,
	})
	if err != nil {
		t.Fatalf("NewApplication() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rr := httptest.NewRecorder()
	application.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}
