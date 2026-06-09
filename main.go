package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mansueli/world-cup-2026-cli-dashboard/data"
	"github.com/mansueli/world-cup-2026-cli-dashboard/data/live"
	"github.com/mansueli/world-cup-2026-cli-dashboard/data/local"
	"github.com/mansueli/world-cup-2026-cli-dashboard/data/supabase"
	"github.com/mansueli/world-cup-2026-cli-dashboard/ui"
)

const defaultSupabaseURL = "https://worldcup.mansueli.com"
const defaultSupabasePublishableKey = "sb_publishable_ZB3uMP-a-C5b8hNQIsxNYA_R0-tn6wr"

func main() {
	fetcher := selectFetcher()
	dashboard := ui.NewDashboard(fetcher, refreshIntervalFromEnv())
	p := tea.NewProgram(dashboard)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Oh no, there's been an error: %v", err)
		os.Exit(1)
	}
}

func selectFetcher() interface {
	GroupTables() ([]data.GroupTable, error)
	SortedMatches() ([]data.Match, error)
	Name() string
} {
	if strings.EqualFold(os.Getenv("WC_DATA_SOURCE"), "supabase") ||
		(strings.TrimSpace(os.Getenv("WC_SUPABASE_URL")) != "" && strings.TrimSpace(os.Getenv("WC_SUPABASE_ANON_KEY")) != "") {
		return supabase.NewClient(
			envOrDefault("WC_SUPABASE_URL", defaultSupabaseURL),
			envOrDefault("WC_SUPABASE_ANON_KEY", defaultSupabasePublishableKey),
			10*time.Second,
		)
	}

	if strings.EqualFold(os.Getenv("WC_DATA_SOURCE"), "local") {
		return &local.Client{}
	}

	return live.NewClient("https://worldcup26.ir", 10*time.Second)
}

func refreshIntervalFromEnv() time.Duration {
	refreshSeconds := 10
	if raw := os.Getenv("WC_REFRESH_SECONDS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err == nil {
			refreshSeconds = parsed
		}
	}

	if refreshSeconds < 5 {
		refreshSeconds = 5
	}
	if refreshSeconds > 15 {
		refreshSeconds = 15
	}

	return time.Duration(refreshSeconds) * time.Second
}

func envOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}
