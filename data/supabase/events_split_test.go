package supabase

import "testing"

func TestSplitEventsByTeamMatchesByID(t *testing.T) {
	// wc.events.team_code stores the upstream team_id, e.g. "1"/"2".
	events := []eventRow{
		{GameID: "1", TeamCode: "1", EventType: "Goal", Minute: "9", Player: "J. Quiñones"},
		{GameID: "1", TeamCode: "1", EventType: "Goal", Minute: "67", Player: "R. Jiménez"},
		{GameID: "1", TeamCode: "2", EventType: "Goal", Minute: "80", Player: "Someone"},
	}

	home := teamMatcher{id: "1", code: "MEX"}
	away := teamMatcher{id: "2", code: "RSA"}

	homeEvents, awayEvents := splitEventsByTeam(events, home, away)
	if len(homeEvents) != 2 {
		t.Fatalf("expected 2 home events, got %d", len(homeEvents))
	}
	if len(awayEvents) != 1 {
		t.Fatalf("expected 1 away event, got %d", len(awayEvents))
	}
}

func TestSplitEventsByTeamMatchesByCodeFallback(t *testing.T) {
	events := []eventRow{
		{GameID: "1", TeamCode: "MEX", EventType: "Goal", Minute: "9", Player: "J. Quiñones"},
	}
	home := teamMatcher{id: "1", code: "MEX"}
	away := teamMatcher{id: "2", code: "RSA"}

	homeEvents, awayEvents := splitEventsByTeam(events, home, away)
	if len(homeEvents) != 1 || len(awayEvents) != 0 {
		t.Fatalf("expected code fallback to map event to home, got home=%d away=%d", len(homeEvents), len(awayEvents))
	}
}
