package supabase

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mansueli/world-cup-2026-cli-dashboard/data"
)

type Client struct {
	baseURL string
	apiKey  string
	schema  string
	client  *http.Client
}

func NewClient(baseURL, apiKey string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	normalizedBaseURL := strings.TrimRight(baseURL, "/")
	normalizedBaseURL = strings.TrimSuffix(normalizedBaseURL, "/rest/v1")

	return &Client{
		baseURL: normalizedBaseURL,
		apiKey:  strings.TrimSpace(apiKey),
		schema:  "wc",
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *Client) Name() string {
	return "supabase"
}

func (c *Client) GroupTables() ([]data.GroupTable, error) {
	teams, err := c.fetchTeams()
	if err != nil {
		return nil, err
	}
	teamByID := map[string]teamRow{}
	for _, team := range teams {
		teamByID[team.TeamID] = team
		code := teamCode(team.FifaCode, team.ISO2, team.TeamID)
		data.SetTeamISO2(code, team.ISO2)
	}

	groups, err := c.fetchGroups()
	if err != nil {
		return nil, err
	}

	out := make([]data.GroupTable, 0, len(groups))
	for _, group := range groups {
		if group.Raw.Name == "" {
			continue
		}

		rows := make([]data.GroupTableTeam, 0, len(group.Raw.Teams))
		for _, gt := range group.Raw.Teams {
			team := teamByID[gt.TeamID]
			code := teamCode(team.FifaCode, team.ISO2, team.TeamID)
			name := team.NameEN
			if strings.TrimSpace(name) == "" {
				name = code
			}

			if _, ok := data.TeamInfoByCode[code]; !ok {
				data.TeamInfoByCode[code] = data.TeamInfo{
					Name:        name,
					Group:       strings.ToUpper(group.Raw.Name),
					FirstColor:  "#1D3557",
					SecondColor: "#F1FAEE",
				}
			}

			rows = append(rows, data.GroupTableTeam{
				Code:              code,
				Points:            atoi(gt.Pts),
				Wins:              atoi(gt.W),
				Draws:             atoi(gt.D),
				Losses:            atoi(gt.L),
				MatchesPlayed:     atoi(gt.MP),
				GoalsFor:          atoi(gt.GF),
				GoalsAgainst:      atoi(gt.GA),
				GoalsDifferential: atoi(gt.GD),
			})
		}

		out = append(out, data.GroupTable{Letter: strings.ToUpper(group.Raw.Name), Table: rows})
	}

	return out, nil
}

func (c *Client) SortedMatches() ([]data.Match, error) {
	teams, err := c.fetchTeams()
	if err != nil {
		return nil, err
	}
	teamByID := map[string]teamRow{}
	for _, team := range teams {
		teamByID[team.TeamID] = team
		code := teamCode(team.FifaCode, team.ISO2, team.TeamID)
		data.SetTeamISO2(code, team.ISO2)
	}

	games, err := c.fetchGames()
	if err != nil {
		return nil, err
	}

	events, err := c.fetchEvents()
	if err != nil {
		return nil, err
	}
	eventsByGameID := make(map[string][]eventRow)
	for _, ev := range events {
		eventsByGameID[ev.GameID] = append(eventsByGameID[ev.GameID], ev)
	}

	matches := make([]data.Match, 0, len(games))
	for _, game := range games {
		home := teamByID[game.HomeTeamID]
		away := teamByID[game.AwayTeamID]

		homeCode := teamCode(home.FifaCode, home.ISO2, home.TeamID)
		awayCode := teamCode(away.FifaCode, away.ISO2, away.TeamID)

		if _, ok := data.TeamInfoByCode[homeCode]; !ok {
			data.TeamInfoByCode[homeCode] = data.TeamInfo{Name: pickName(home.NameEN, homeCode), Group: strings.ToUpper(game.GroupName), FirstColor: "#1D3557", SecondColor: "#F1FAEE"}
		}
		if _, ok := data.TeamInfoByCode[awayCode]; !ok {
			data.TeamInfoByCode[awayCode] = data.TeamInfo{Name: pickName(away.NameEN, awayCode), Group: strings.ToUpper(game.GroupName), FirstColor: "#1D3557", SecondColor: "#F1FAEE"}
		}

		gameEvents := eventsByGameID[game.GameID]
		homeEvents, awayEvents := splitEventsByTeam(
			gameEvents,
			teamMatcher{id: home.TeamID, code: homeCode},
			teamMatcher{id: away.TeamID, code: awayCode},
		)

		status, minute := mapStatus(game.Finished, game.TimeElapsed)
		matches = append(matches, data.Match{
			ID:             atoi(game.GameID),
			HomeTeamCode:   homeCode,
			AwayTeamCode:   awayCode,
			Date:           parseDate(game.KickoffAt, game.LocalDateRaw),
			HomeTeamScore:  scoreToUint64(game.HomeScore),
			AwayTeamScore:  scoreToUint64(game.AwayScore),
			Status:         status,
			Minute:         minute,
			Stage:          mapStage(game.Stage),
			HomeTeamEvents: homeEvents,
			AwayTeamEvents: awayEvents,
		})
	}

	return matches, nil
}

// IsLiveOrSoon reports whether the sync pipeline considers a match to be live
// or starting soon, based on the wc.sync_state singleton row. When the row is
// missing or the request fails, it returns false so callers fall back to the
// slower idle polling cadence.
func (c *Client) IsLiveOrSoon() (bool, error) {
	rows := []syncStateRow{}
	if err := c.get("sync_state", &rows, "select=is_live_or_soon&limit=1"); err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	return rows[0].IsLiveOrSoon, nil
}

func (c *Client) fetchTeams() ([]teamRow, error) {
	rows := []teamRow{}
	if err := c.get("teams", &rows, "select=team_id,fifa_code,iso2,group_name,name_en"); err != nil {
		return nil, err
	}
	return rows, nil
}

func (c *Client) fetchGames() ([]gameRow, error) {
	rows := []gameRow{}
	query := "select=game_id,home_team_id,away_team_id,group_name,stage,finished,time_elapsed,local_date_raw,kickoff_at,home_score,away_score&order=kickoff_at.asc"
	if err := c.get("games", &rows, query); err != nil {
		return nil, err
	}
	return rows, nil
}

func (c *Client) fetchGroups() ([]groupRow, error) {
	rows := []groupRow{}
	if err := c.get("groups", &rows, "select=group_name,raw"); err != nil {
		return nil, err
	}
	return rows, nil
}

func (c *Client) fetchEvents() ([]eventRow, error) {
	rows := []eventRow{}
	query := "select=game_id,team_code,event_type,minute,player,canceled"
	err := c.get("events", &rows, query)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "relation") {
			return []eventRow{}, nil
		}
		return nil, err
	}
	return rows, nil
}

// teamMatcher identifies a match side by both its raw team_id and its derived
// display code. The wc.events.team_code column stores the upstream team_id
// (e.g. "1"), not the 3-letter FIFA code, so we match on the id first and fall
// back to the code for resilience against future data shapes.
type teamMatcher struct {
	id   string
	code string
}

func (t teamMatcher) matches(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return false
	}
	if strings.EqualFold(v, strings.TrimSpace(t.id)) {
		return true
	}
	return strings.EqualFold(v, strings.TrimSpace(t.code))
}

func splitEventsByTeam(events []eventRow, home, away teamMatcher) (homeEvents, awayEvents []data.Event) {
	for _, ev := range events {
		event := data.Event{
			Type:     ev.EventType,
			Minute:   ev.Minute,
			Player:   ev.Player,
			Canceled: ev.Canceled,
		}
		switch {
		case home.matches(ev.TeamCode):
			homeEvents = append(homeEvents, event)
		case away.matches(ev.TeamCode):
			awayEvents = append(awayEvents, event)
		}
	}
	return homeEvents, awayEvents
}

func (c *Client) get(table string, out any, query string) error {
	u := fmt.Sprintf("%s/rest/v1/%s?%s", c.baseURL, table, query)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	req.Header.Set("apikey", c.apiKey)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept-Profile", c.schema)
	req.Header.Set("Content-Profile", c.schema)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("supabase %s returned status %d", table, resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func mapStatus(finished bool, elapsed string) (data.Status, string) {
	if finished {
		return data.StatusFinished, ""
	}
	normalized := strings.TrimSpace(strings.ToLower(elapsed))
	if normalized == "" || normalized == "notstarted" || normalized == "ns" {
		return data.StatusScheduled, ""
	}
	return data.StatusLive, elapsed
}

func mapStage(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "group":
		return string(data.StageGroup)
	case "r16":
		return string(data.StageLast16)
	case "qf":
		return string(data.StageQuarter)
	case "sf":
		return string(data.StageSemi)
	case "third":
		return string(data.StageThird)
	case "final":
		return string(data.StageFinal)
	default:
		if strings.TrimSpace(raw) == "" {
			return "Unknown"
		}
		return strings.ToUpper(strings.TrimSpace(raw))
	}
}

func parseDate(kickoffAt, localDateRaw string) time.Time {
	if strings.TrimSpace(kickoffAt) != "" {
		if t, err := time.Parse(time.RFC3339, kickoffAt); err == nil {
			return t.Local()
		}
	}
	if t, err := time.ParseInLocation("01/02/2006 15:04", strings.TrimSpace(localDateRaw), time.Local); err == nil {
		return t
	}
	return time.Now()
}

func teamCode(fifaCode, iso2, teamID string) string {
	code := strings.ToUpper(strings.TrimSpace(fifaCode))
	if len(code) == 3 {
		return code
	}
	iso := strings.ToUpper(strings.TrimSpace(iso2))
	if len(iso) == 3 {
		return iso
	}
	if len(iso) == 2 {
		return "X" + iso
	}
	return "T" + strings.ToUpper(strings.TrimSpace(teamID))
}

func pickName(name, fallback string) string {
	if strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return fallback
}

func atoi(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}

func scoreToUint64(score int) uint64 {
	if score < 0 {
		return 0
	}
	return uint64(score)
}

type syncStateRow struct {
	IsLiveOrSoon bool `json:"is_live_or_soon"`
}

type groupRow struct {
	GroupName string       `json:"group_name"`
	Raw       groupRawData `json:"raw"`
}

type groupRawData struct {
	Name  string             `json:"name"`
	Teams []groupRawTeamData `json:"teams"`
}

type groupRawTeamData struct {
	TeamID string `json:"team_id"`
	MP     string `json:"mp"`
	W      string `json:"w"`
	L      string `json:"l"`
	D      string `json:"d"`
	Pts    string `json:"pts"`
	GF     string `json:"gf"`
	GA     string `json:"ga"`
	GD     string `json:"gd"`
}

type teamRow struct {
	TeamID    string `json:"team_id"`
	FifaCode  string `json:"fifa_code"`
	ISO2      string `json:"iso2"`
	GroupName string `json:"group_name"`
	NameEN    string `json:"name_en"`
}

type gameRow struct {
	GameID       string `json:"game_id"`
	HomeTeamID   string `json:"home_team_id"`
	AwayTeamID   string `json:"away_team_id"`
	GroupName    string `json:"group_name"`
	Stage        string `json:"stage"`
	Finished     bool   `json:"finished"`
	TimeElapsed  string `json:"time_elapsed"`
	LocalDateRaw string `json:"local_date_raw"`
	KickoffAt    string `json:"kickoff_at"`
	HomeScore    int    `json:"home_score"`
	AwayScore    int    `json:"away_score"`
}

type eventRow struct {
	GameID    string `json:"game_id"`
	TeamCode  string `json:"team_code"`
	EventType string `json:"event_type"`
	Minute    string `json:"minute"`
	Player    string `json:"player"`
	Canceled  bool   `json:"canceled"`
}
