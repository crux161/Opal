package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"opal/internal/omiai"
	"opal/internal/store"
	"opal/internal/ui"
)

func main() {
	cfg := ui.Config{
		APIURL:       flagOrEnv("api-url", "OPAL_API_URL", "http://127.0.0.1:8000", "Omiai API base URL"),
		SignalingURL: flagOrEnv("signal-url", "OPAL_SIGNAL_URL", "ws://127.0.0.1:4000/ws/sankaku/websocket", "Omiai signaling websocket URL"),
	}
	flag.Parse()

	sessionStore, err := store.New("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to prepare session store: %v\n", err)
		os.Exit(1)
	}

	initialSession, err := sessionStore.Load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "failed to load saved session: %v\n", err)
		os.Exit(1)
	}

	cfg.Store = sessionStore
	cfg.API = omiai.NewAPIClient(cfg.APIURL)
	cfg.InitialSession = initialSession

	program := tea.NewProgram(ui.NewModel(cfg), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "opal exited with error: %v\n", err)
		os.Exit(1)
	}
}

func flagOrEnv(name, envKey, fallback, usage string) string {
	value := fallback
	if envValue := os.Getenv(envKey); envValue != "" {
		value = envValue
	}
	return *flag.String(name, value, usage)
}
