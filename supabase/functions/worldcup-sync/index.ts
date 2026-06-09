// @ts-nocheck
import { createClient } from "https://esm.sh/@supabase/supabase-js@2";

type ApiGame = {
  id: string;
  home_team_id: string;
  away_team_id: string;
  home_score: string;
  away_score: string;
  group: string;
  local_date: string;
  finished: string;
  time_elapsed: string;
  type: string;
};

type ApiTeam = {
  id: string;
  name_en: string;
  fifa_code: string;
  iso2: string;
  groups: string;
};

type ApiGroup = {
  name: string;
};

const API_BASE = Deno.env.get("WC_API_BASE") ?? "https://worldcup26.ir";
const WINDOW_MINUTES = Number(Deno.env.get("WC_WINDOW_MINUTES") ?? "15");

const SUPABASE_URL = Deno.env.get("SUPABASE_URL") ?? "";
const SERVICE_ROLE_KEY = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY") ?? "";

if (!SUPABASE_URL || !SERVICE_ROLE_KEY) {
  throw new Error("Missing SUPABASE_URL or SUPABASE_SERVICE_ROLE_KEY");
}

const supabase = createClient(SUPABASE_URL, SERVICE_ROLE_KEY);

function parseKickoff(raw: string): Date | null {
  // worldcup26 uses MM/DD/YYYY HH:mm
  const parts = raw.trim().split(" ");
  if (parts.length !== 2) return null;

  const datePart = parts[0].split("/");
  const timePart = parts[1].split(":");
  if (datePart.length !== 3 || timePart.length !== 2) return null;

  const month = Number(datePart[0]);
  const day = Number(datePart[1]);
  const year = Number(datePart[2]);
  const hour = Number(timePart[0]);
  const minute = Number(timePart[1]);

  if (
    [month, day, year, hour, minute].some((n) => Number.isNaN(n)) ||
    month < 1 ||
    month > 12 ||
    day < 1 ||
    day > 31 ||
    hour < 0 ||
    hour > 23 ||
    minute < 0 ||
    minute > 59
  ) {
    return null;
  }

  // Store as UTC for consistent scheduling checks.
  return new Date(Date.UTC(year, month - 1, day, hour, minute, 0));
}

function isLive(game: ApiGame): boolean {
  const elapsed = (game.time_elapsed ?? "").trim().toLowerCase();
  const finished = (game.finished ?? "").trim().toLowerCase() === "true";
  if (finished) return false;
  return elapsed !== "" && elapsed !== "notstarted" && elapsed !== "ns";
}

function isSoon(game: ApiGame, now: Date): boolean {
  const kickoff = parseKickoff(game.local_date);
  if (!kickoff) return false;

  const finished = (game.finished ?? "").trim().toLowerCase() === "true";
  if (finished) return false;

  const deltaMs = kickoff.getTime() - now.getTime();
  return deltaMs >= 0 && deltaMs <= WINDOW_MINUTES * 60_000;
}

function nextKickoff(games: ApiGame[], now: Date): Date | null {
  let next: Date | null = null;
  for (const game of games) {
    const kickoff = parseKickoff(game.local_date);
    if (!kickoff) continue;

    const finished = (game.finished ?? "").trim().toLowerCase() === "true";
    if (finished || kickoff.getTime() < now.getTime()) continue;

    if (!next || kickoff.getTime() < next.getTime()) {
      next = kickoff;
    }
  }
  return next;
}

async function fetchJSON<T>(path: string): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`);
  if (!response.ok) {
    throw new Error(`Failed ${path}: ${response.status}`);
  }
  return (await response.json()) as T;
}

Deno.serve(async () => {
  try {
    const [gamesPayload, teamsPayload, groupsPayload] = await Promise.all([
      fetchJSON<{ games: ApiGame[] }>("/get/games"),
      fetchJSON<{ teams: ApiTeam[] }>("/get/teams"),
      fetchJSON<{ groups: ApiGroup[] }>("/get/groups"),
    ]);

    const games = gamesPayload.games ?? [];
    const teams = teamsPayload.teams ?? [];
    const groups = groupsPayload.groups ?? [];
    const now = new Date();

    const activeWindow = games.some((game) => isLive(game) || isSoon(game, now));
    const next = nextKickoff(games, now);

    await supabase.schema("wc").rpc("set_sync_state", {
      p_is_live_or_soon: activeWindow,
      p_next_kickoff: next ? next.toISOString() : null,
    });

    // Keep base data cached even when inactive. Broadcast trigger is gated by sync_state.
    const teamRows = teams.map((team) => ({
      team_id: team.id,
      fifa_code: team.fifa_code,
      iso2: team.iso2,
      group_name: team.groups,
      name_en: team.name_en,
      raw: team,
      updated_at: new Date().toISOString(),
    }));

    const gameRows = games.map((game) => ({
      game_id: game.id,
      home_team_id: game.home_team_id,
      away_team_id: game.away_team_id,
      group_name: game.group,
      stage: game.type,
      finished: (game.finished ?? "").trim().toLowerCase() === "true",
      time_elapsed: game.time_elapsed,
      local_date_raw: game.local_date,
      kickoff_at: parseKickoff(game.local_date)?.toISOString() ?? null,
      home_score: Number(game.home_score || "0"),
      away_score: Number(game.away_score || "0"),
      raw: game,
      updated_at: new Date().toISOString(),
    }));

    const groupRows = groups.map((group) => ({
      group_name: group.name,
      raw: group,
      updated_at: new Date().toISOString(),
    }));

    if (teamRows.length > 0) {
      const { error } = await supabase.schema("wc").from("teams").upsert(teamRows, { onConflict: "team_id" });
      if (error) throw error;
    }

    if (gameRows.length > 0) {
      const { error } = await supabase.schema("wc").from("games").upsert(gameRows, { onConflict: "game_id" });
      if (error) throw error;
    }

    if (groupRows.length > 0) {
      const { error } = await supabase.schema("wc").from("groups").upsert(groupRows, { onConflict: "group_name" });
      if (error) throw error;
    }

    return new Response(
      JSON.stringify({
        ok: true,
        live_or_soon: activeWindow,
        next_kickoff: next ? next.toISOString() : null,
        counts: {
          teams: teamRows.length,
          games: gameRows.length,
          groups: groupRows.length,
        },
      }),
      {
        headers: { "content-type": "application/json" },
      },
    );
  } catch (error) {
    const message = error instanceof Error ? error.message : "Unknown error";
    return new Response(JSON.stringify({ ok: false, error: message }), {
      status: 500,
      headers: { "content-type": "application/json" },
    });
  }
});
