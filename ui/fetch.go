package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mansueli/world-cup-2026-cli-dashboard/data"
	"github.com/mansueli/world-cup-2026-cli-dashboard/ui/playerstats"
)

type dataFetcher interface {
	GroupTables() ([]data.GroupTable, error)
	SortedMatches() ([]data.Match, error)
	Name() string
}

// liveStatusFetcher is an optional capability. Fetchers that can report whether
// a match is live or starting soon (e.g. the Supabase source via wc.sync_state)
// implement it so the dashboard can poll faster during live windows.
type liveStatusFetcher interface {
	IsLiveOrSoon() (bool, error)
}

type dataFetchMsg struct {
	groupTablesByLetter map[string]data.GroupTable
	sortedMatches       []data.Match
	playerStatsByTeam   map[string]playerstats.PlayerStats
	isLiveOrSoon        bool
}

type dataFetchErrMsg struct{ err error }

func dataFetchCmd(fetcher dataFetcher) func() tea.Msg {
	return func() tea.Msg {
		groupTables, err := fetcher.GroupTables()
		if err != nil {
			return dataFetchErrMsg{err: err}
		}
		groupTablesByLetter := make(map[string]data.GroupTable, len(groupTables))
		for _, g := range groupTables {
			groupTablesByLetter[g.Letter] = g
		}

		sortedMatches, err := fetcher.SortedMatches()
		if err != nil {
			return dataFetchErrMsg{err: err}
		}

		playerStatsByTeam := playerstats.PlayerStatsByTeam(sortedMatches)

		isLiveOrSoon := anyMatchLive(sortedMatches)
		if probe, ok := fetcher.(liveStatusFetcher); ok {
			if live, err := probe.IsLiveOrSoon(); err == nil {
				isLiveOrSoon = live
			}
		}

		return dataFetchMsg{groupTablesByLetter, sortedMatches, playerStatsByTeam, isLiveOrSoon}
	}
}

func anyMatchLive(matches []data.Match) bool {
	for _, m := range matches {
		if m.Status == data.StatusLive {
			return true
		}
	}
	return false
}
