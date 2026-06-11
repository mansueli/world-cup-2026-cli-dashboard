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

		status, minute := mapStatus(game.Finished, game.TimeElapsed)

		homeEvents, awayEvents := parseEventsFromRaw(game.Raw, homeCode, awayCode)

		matches = append(matches, data.Match{
			ID:               atoi(game.GameID),
			HomeTeamCode:     homeCode,
			AwayTeamCode:     awayCode,
			Date:             parseDate(game.KickoffAt, game.LocalDateRaw),
			HomeTeamScore:    scoreToUint64(game.HomeScore),
			AwayTeamScore:    scoreToUint64(game.AwayScore),
			Status:           status,
			Minute:           minute,
			Stage:            mapStage(game.Stage),
			HomeTeamEvents:   homeEvents,
			AwayTeamEvents:   awayEvents,
			// Lineups can be parsed similarly from game.Raw if present in the upstream payload
		})
	}

	return matches, nil
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
	// Include raw so we can extract events, lineups, etc. from the original upstream payload
	query := "select=game_id,home_team_id,away_team_id,group_name,stage,finished,time_elapsed,local_date_raw,kickoff_at,home_score,away_score,raw&order=kickoff_at.asc"
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

// parseEventsFromRaw attempts to extract events from the stored raw JSONB payload.
// It supports a few common shapes:
//   - { "events": [ {type, minute, player, canceled, team?} ] }
//   - { "home_events": [...], "away_events": [...] }
// Adjust the struct tags / logic to match the actual upstream payload shape.
func parseEventsFromRaw(raw json.RawMessage, homeCode, awayCode string) (homeEvents, awayEvents []data.Event) {
	if len(raw) == 0 {
		return nil, nil
	}

	// Try unified events list first
	var unified struct {
		Events []struct {
			Type     string `json:"type"`
			Minute   any    `json:"minute"`
			Player   string `json:"player"`
			Canceled bool   `json:"canceled"`
			Team     string `json:"team"` // "home", "away", or team code
		} `json:"events"`
	}
	if err := json.Unmarshal(raw, &unified); err == nil && len(unified.Events) > 0 {
		for _, e := range unified.Events {
			ev := data.Event{
				Type:     strings.TrimSpace(e.Type),
				Player:   strings.TrimSpace(e.Player),
				Canceled: e.Canceled,
			}
			switch v := e.Minute.(type) {
			case float64:
				ev.Minute = strconv.FormatFloat(v, 'f', 0, 64)
			case string:
				ev.Minute = strings.TrimSpace(v)
			}

			side := strings.ToLower(strings.TrimSpace(e.Team))
			if side == "home" || side == homeCode {
				homeEvents = append(homeEvents, ev)
			} else if side == "away" || side == awayCode {
				awayEvents = append(awayEvents, ev)
			} else {
				// If no team info, put in both or skip (conservative: put in home for now)
				homeEvents = append(homeEvents, ev)
			}
		}
		return homeEvents, awayEvents
	}

	// Try separate home/away event arrays
	var separated struct {
		HomeEvents []struct {
			Type     string `json:"type"`
			Minute   any    `json:"minute"`
			Player   string `json:"player"`
			Canceled bool   `json:"canceled"`
		} `json:"home_events"`
		AwayEvents []struct {
			Type     string `json:"type"`
			Minute   any    `json:"minute"`
			Player   string `json:"player"`
			Canceled bool   `json:"canceled"`
		} `json:"away_events"`
	}
	if err := json.Unmarshal(raw, &separated); err == nil {
		for _, e := range separated.HomeEvents {
			ev := data.Event{Type: strings.TrimSpace(e.Type), Player: strings.TrimSpace(e.Player), Canceled: e.Canceled}
			switch v := e.Minute.(type) {
			case float64:
				ev.Minute = strconv.FormatFloat(v, 'f', 0, 64)
			case string:
				ev.Minute = strings.TrimSpace(v)
			}
			homeEvents = append(homeEvents, ev)
		}
		for _, e := range separated.AwayEvents {
			ev := data.Event{Type: strings.TrimSpace(e.Type), Player: strings.TrimSpace(e.Player), Canceled: e.Canceled}
			switch v := e.Minute.(type) {
			case float64:
				ev.Minute = strconv.FormatFloat(v, 'f', 0, 64)
			case string:
				ev.Minute = strings.TrimSpace(v)
			}
			awayEvents = append(awayEvents, ev)
		}
		return homeEvents, awayEvents
	}

	// No recognized event shape found — events will be empty (yellow cards won't appear until data shape matches)
	return nil, nil
}

type gameRow struct {
	GameID       string          `json:"game_id"`
	HomeTeamID   string          `json:"home_team_id"`
	AwayTeamID   string          `json:"away_team_id"`
	GroupName    string          `json:"group_name"`
	Stage        string          `json:"stage"`
	Finished     bool            `json:"finished"`
	TimeElapsed  string          `json:"time_elapsed"`
	LocalDateRaw string          `json:"local_date_raw"`
	KickoffAt    string          `json:"kickoff_at"`
	HomeScore    int             `json:"home_score"`
	AwayScore    int             `json:"away_score"`
	Raw          json.RawMessage `json:"raw"`
}

type teamRow struct {
	TeamID    string `json:"team_id"`
	FifaCode  string `json:"fifa_code"`
	ISO2      string `json:"iso2"`
	GroupName string `json:"group_name"`
	NameEN    string `json:"name_en"`
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
