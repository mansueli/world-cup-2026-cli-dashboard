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
	liveInterval, idleInterval := refreshIntervalsFromEnv()
	dashboard := ui.NewDashboard(fetcher, liveInterval, idleInterval)
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
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WC_DATA_SOURCE"))) {
	case "live":
		return live.NewClient("https://worldcup26.ir", 10*time.Second)
	case "local":
		return &local.Client{}
	}

	// Supabase is the default data source. The CLI polls the wc.games table
	// (and related tables) directly, using wc.sync_state to decide how often.
	return supabase.NewClient(
		envOrDefault("WC_SUPABASE_URL", defaultSupabaseURL),
		envOrDefault("WC_SUPABASE_ANON_KEY", defaultSupabasePublishableKey),
		10*time.Second,
	)
}

// refreshIntervalsFromEnv returns the polling cadence used while a match is
// live or starting soon, and the slower cadence used when idle.
//
//	WC_REFRESH_SECONDS      live cadence  (default 3, clamped 1-15)
//	WC_IDLE_REFRESH_SECONDS idle cadence  (default 30, clamped 10-300)
func refreshIntervalsFromEnv() (live, idle time.Duration) {
	liveSeconds := clampInt(envInt("WC_REFRESH_SECONDS", 3), 1, 15)
	idleSeconds := clampInt(envInt("WC_IDLE_REFRESH_SECONDS", 30), 10, 300)
	if idleSeconds < liveSeconds {
		idleSeconds = liveSeconds
	}
	return time.Duration(liveSeconds) * time.Second, time.Duration(idleSeconds) * time.Second
}

func envInt(name string, fallback int) int {
	if raw := strings.TrimSpace(os.Getenv(name)); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			return parsed
		}
	}
	return fallback
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func envOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}
