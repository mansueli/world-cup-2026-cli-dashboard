package live

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mansueli/world-cup-2026-cli-dashboard/data"
)

type Client struct {
	baseURL    string
	httpClient *http.Client

	mu          sync.RWMutex
	teamByID    map[string]teamAPI
	teamsCached time.Time
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		teamByID: make(map[string]teamAPI),
	}
}

func (c *Client) Name() string {
	return "worldcup26.ir"
}

func (c *Client) GroupTables() ([]data.GroupTable, error) {
	teamByID, err := c.fetchTeamsMap()
	if err != nil {
		return nil, err
	}

	var payload groupsResponse
	if err := c.getJSON("/get/groups", &payload); err != nil {
		return nil, err
	}

	out := make([]data.GroupTable, 0, len(payload.Groups))
	for _, g := range payload.Groups {
		table := make([]data.GroupTableTeam, 0, len(g.Teams))
		for _, entry := range g.Teams {
			team := teamByID[entry.TeamID]
			code := teamCode(team, entry.TeamID)
			ensureTeamInfo(code, team.NameEN, g.Name, team.ISO2)

			table = append(table, data.GroupTableTeam{
				Code:              code,
				Points:            atoi(entry.Pts),
				Wins:              atoi(entry.W),
				Draws:             atoi(entry.D),
				Losses:            atoi(entry.L),
				MatchesPlayed:     atoi(entry.MP),
				GoalsFor:          atoi(entry.GF),
				GoalsAgainst:      atoi(entry.GA),
				GoalsDifferential: atoi(entry.GD),
			})
		}

		sort.Slice(table, func(i, j int) bool {
			if table[i].Points != table[j].Points {
				return table[i].Points > table[j].Points
			}
			if table[i].GoalsDifferential != table[j].GoalsDifferential {
				return table[i].GoalsDifferential > table[j].GoalsDifferential
			}
			if table[i].GoalsFor != table[j].GoalsFor {
				return table[i].GoalsFor > table[j].GoalsFor
			}
			return table[i].Code < table[j].Code
		})

		out = append(out, data.GroupTable{
			Letter: strings.ToUpper(strings.TrimSpace(g.Name)),
			Table:  table,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Letter < out[j].Letter
	})

	return out, nil
}

func (c *Client) SortedMatches() ([]data.Match, error) {
	teamByID, err := c.fetchTeamsMap()
	if err != nil {
		return nil, err
	}

	var payload gamesResponse
	if err := c.getJSON("/get/games", &payload); err != nil {
		return nil, err
	}

	matches := make([]data.Match, 0, len(payload.Games))
	for _, g := range payload.Games {
		homeTeam := teamByID[g.HomeTeamID]
		awayTeam := teamByID[g.AwayTeamID]

		homeCode := teamCode(homeTeam, g.HomeTeamID)
		awayCode := teamCode(awayTeam, g.AwayTeamID)

		ensureTeamInfo(homeCode, pickFirst(homeTeam.NameEN, g.HomeTeamNameEN, homeCode), g.Group, homeTeam.ISO2)
		ensureTeamInfo(awayCode, pickFirst(awayTeam.NameEN, g.AwayTeamNameEN, awayCode), g.Group, awayTeam.ISO2)

		matchDate := parseLocalDate(g.LocalDate)
		status, minute := mapStatus(g.Finished, g.TimeElapsed)

		matches = append(matches, data.Match{
			ID:            atoi(g.ID),
			HomeTeamCode:  homeCode,
			AwayTeamCode:  awayCode,
			Date:          matchDate,
			Venue:         "",
			HomeTeamScore: atou64(g.HomeScore),
			AwayTeamScore: atou64(g.AwayScore),
			Minute:        minute,
			Status:        status,
			Stage:         mapStage(g.Type),
		})
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Date.Equal(matches[j].Date) {
			return matches[i].ID < matches[j].ID
		}
		return matches[i].Date.Before(matches[j].Date)
	})

	return matches, nil
}

func (c *Client) fetchTeamsMap() (map[string]teamAPI, error) {
	c.mu.RLock()
	if len(c.teamByID) > 0 && time.Since(c.teamsCached) < 15*time.Minute {
		cached := make(map[string]teamAPI, len(c.teamByID))
		for id, t := range c.teamByID {
			cached[id] = t
		}
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()

	var payload teamsResponse
	if err := c.getJSON("/get/teams", &payload); err != nil {
		return nil, err
	}

	fresh := make(map[string]teamAPI, len(payload.Teams))
	for _, team := range payload.Teams {
		fresh[team.ID] = team
		ensureTeamInfo(teamCode(team, team.ID), team.NameEN, team.Group, team.ISO2)
	}

	c.mu.Lock()
	c.teamByID = fresh
	c.teamsCached = time.Now()
	c.mu.Unlock()

	out := make(map[string]teamAPI, len(fresh))
	for id, team := range fresh {
		out[id] = team
	}
	return out, nil
}

func (c *Client) getJSON(path string, out any) error {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%s returned status %d", path, resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func mapStatus(finishedRaw, elapsedRaw string) (data.Status, string) {
	finished := strings.EqualFold(strings.TrimSpace(finishedRaw), "true")
	elapsed := strings.TrimSpace(elapsedRaw)
	if finished {
		return data.StatusFinished, ""
	}
	if elapsed == "" || strings.EqualFold(elapsed, "notstarted") || strings.EqualFold(elapsed, "ns") {
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
		stage := strings.ToUpper(strings.TrimSpace(raw))
		if stage == "" {
			return "Unknown"
		}
		return stage
	}
}

func parseLocalDate(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now()
	}

	for _, layout := range []string{"01/02/2006 15:04", "2006-01-02 15:04", time.RFC3339} {
		t, err := time.ParseInLocation(layout, raw, time.Local)
		if err == nil {
			return t
		}
	}

	return time.Now()
}

func teamCode(team teamAPI, fallbackID string) string {
	code := strings.ToUpper(strings.TrimSpace(team.FifaCode))
	if len(code) == 3 {
		return code
	}

	iso := strings.ToUpper(strings.TrimSpace(team.ISO2))
	if len(iso) == 3 {
		return iso
	}

	if len(iso) == 2 {
		return "X" + iso
	}

	nameCode := nameToCode(team.NameEN)
	if nameCode != "" {
		return nameCode
	}

	return "T" + strings.ToUpper(strings.TrimSpace(fallbackID))
}

func ensureTeamInfo(code, name, group, iso2 string) {
	if code == "" {
		return
	}

	data.SetTeamISO2(code, iso2)

	if info, ok := data.TeamInfoByCode[code]; ok {
		if info.Name == "" && strings.TrimSpace(name) != "" {
			info.Name = strings.TrimSpace(name)
			data.TeamInfoByCode[code] = info
		}
		return
	}

	firstColor, secondColor := paletteForCode(code)
	teamName := strings.TrimSpace(name)
	if teamName == "" {
		teamName = code
	}

	data.TeamInfoByCode[code] = data.TeamInfo{
		Name:        teamName,
		Group:       strings.ToUpper(strings.TrimSpace(group)),
		FirstColor:  firstColor,
		SecondColor: secondColor,
	}
}

func paletteForCode(code string) (string, string) {
	pairs := [][2]string{
		{"#EF233C", "#2B2D42"},
		{"#0B6E4F", "#D9ED92"},
		{"#1D3557", "#F1FAEE"},
		{"#003049", "#F77F00"},
		{"#6A4C93", "#F2E9E4"},
		{"#2A9D8F", "#264653"},
		{"#9A031E", "#FFBA08"},
		{"#005F73", "#E9D8A6"},
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(code))
	idx := int(h.Sum32()) % len(pairs)
	return pairs[idx][0], pairs[idx][1]
}

func nameToCode(name string) string {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return ""
	}

	parts := strings.Fields(name)
	if len(parts) >= 3 {
		code := string(parts[0][0]) + string(parts[1][0]) + string(parts[2][0])
		return strings.Map(onlyAlphaNum, code)
	}

	clean := strings.Map(onlyAlphaNum, name)
	if len(clean) >= 3 {
		return clean[:3]
	}
	return clean
}

func onlyAlphaNum(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r
	}
	if r >= '0' && r <= '9' {
		return r
	}
	return -1
}

func atoi(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return n
}

func atou64(raw string) uint64 {
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func pickFirst(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type teamsResponse struct {
	Teams []teamAPI `json:"teams"`
}

type teamAPI struct {
	ID       string `json:"id"`
	NameEN   string `json:"name_en"`
	FifaCode string `json:"fifa_code"`
	ISO2     string `json:"iso2"`
	Group    string `json:"groups"`
}

type groupsResponse struct {
	Groups []groupAPI `json:"groups"`
}

type groupAPI struct {
	Name  string            `json:"name"`
	Teams []groupTeamAPIRow `json:"teams"`
}

type groupTeamAPIRow struct {
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

type gamesResponse struct {
	Games []gameAPI `json:"games"`
}

type gameAPI struct {
	ID             string `json:"id"`
	HomeTeamID     string `json:"home_team_id"`
	AwayTeamID     string `json:"away_team_id"`
	HomeScore      string `json:"home_score"`
	AwayScore      string `json:"away_score"`
	Group          string `json:"group"`
	LocalDate      string `json:"local_date"`
	Finished       string `json:"finished"`
	TimeElapsed    string `json:"time_elapsed"`
	Type           string `json:"type"`
	HomeTeamNameEN string `json:"home_team_name_en"`
	AwayTeamNameEN string `json:"away_team_name_en"`
}
