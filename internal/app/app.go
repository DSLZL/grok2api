package app

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"grok2api/internal/config"
	httpserver "grok2api/internal/http"
)

type Application struct {
	config  ServerConfig
	handler http.Handler
}

func NewApplication(cfg ServerConfig) (*Application, error) {
	configManager, err := buildConfigManager(cfg.ConfigPath)
	if err != nil {
		return nil, err
	}

	handler := httpserver.NewRouter(httpserver.RouterOptions{
		StaticDir: "app/static",
		Config:    configManager,
	})

	return &Application{
		config:  cfg,
		handler: handler,
	}, nil
}

func buildConfigManager(configPath string) (*config.Manager, error) {
	defaults := map[string]any{
		"app": map[string]any{
			"public_enabled": false,
		},
	}

	seedPath := filepath.Join("data", "config.toml")
	if configPath != "" {
		seedPath = configPath
	}

	seedLoader := func() (map[string]any, error) {
		parsed, err := config.ParseTOMLFile(seedPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return map[string]any{}, nil
			}
			return nil, err
		}
		return parsed, nil
	}

	manager := config.NewManager(&appConfigStore{}, defaults, seedLoader)
	if err := manager.Load(); err != nil {
		return nil, fmt.Errorf("load app config: %w", err)
	}

	return manager, nil
}

type appConfigStore struct{}

func (s *appConfigStore) LoadConfig() (map[string]any, error)  { return nil, nil }
func (s *appConfigStore) SaveConfig(data map[string]any) error { return nil }
func (s *appConfigStore) AcquireLock(name string, timeout time.Duration) (func() error, error) {
	return func() error { return nil }, nil
}

func (a *Application) Address() string {
	return fmt.Sprintf("%s:%d", a.config.Host, a.config.Port)
}

func (a *Application) Handler() http.Handler {
	return a.handler
}
