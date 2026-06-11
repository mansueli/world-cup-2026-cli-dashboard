// @ts-nocheck
import { createClient } from "npm:@supabase/supabase-js@2";

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

type ApiEvent = {
  id: number | string;
  game_id?: string;
  team_id?: string;
  team_code?: string;
  type: string;
  minute?: string;
  player?: string;
  canceled?: boolean;
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

type FallbackGame = {
  id: number | string;
  home_team_country?: string;
  away_team_country?: string;
  home_team?: { country: string; name: string; goals?: number };
  away_team?: { country: string; name: string; goals?: number };
  datetime?: string;
  status?: string;
  stage_name?: string;
  // ... other fields
};

type FallbackTeamGroup = {
  letter: string;
  teams: Array<{
    country: string;
    name: string;
    group_letter?: string;
    // ... other stats
  }>;
};

const API_BASE = "https://worldcup26.ir";
const FALLBACK_BASE = "https://worldcupjson.net";
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
    month < 1 || month > 12 ||
    day < 1 || day > 31 ||
    hour < 0 || hour > 23 ||
    minute < 0 || minute > 59
  ) {
    return null;
  }

  return new Date(Date.UTC(year, month - 1, day, hour, minute, 0));
}

// Fallback kickoff parser (ISO datetime from worldcupjson.net)
function parseFallbackKickoff(datetime: string | undefined): Date | null {
  if (!datetime) return null;
  const date = new Date(datetime);
  return isNaN(date.getTime()) ? null : date;
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

async function fetchJSON<T>(url: string): Promise<T> {
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`Failed ${url}: ${response.status}`);
  }
  return (await response.json()) as T;
}

async function fetchWithFallback() {
  try {
    // Primary provider
    const [gamesPayload, teamsPayload, groupsPayload, eventsPayload] = await Promise.all([
      fetchJSON<{ games: ApiGame[] }>(`${API_BASE}/get/games`),
      fetchJSON<{ teams: ApiTeam[] }>(`${API_BASE}/get/teams`),
      fetchJSON<{ groups: ApiGroup[] }>(`${API_BASE}/get/groups`),
      fetchJSON<{ events: ApiEvent[] }>(`${API_BASE}/get/events`).catch(() => ({ events: [] })),
    ]);

    console.log("✅ Using primary provider (worldcup26.ir)");
    return {
      games: gamesPayload.games ?? [],
      teams: teamsPayload.teams ?? [],
      groups: groupsPayload.groups ?? [],
      events: eventsPayload?.events ?? [],
      source: "primary",
    };
  } catch (primaryError) {
    console.error("⚠️ Primary provider failed:", primaryError.message);
    console.log("🔄 Falling back to worldcupjson.net...");
  }

  // Fallback
  try {
    const [matches, teamsData] = await Promise.all([
      fetchJSON<FallbackGame[]>(`${FALLBACK_BASE}/matches`),
      fetchJSON<{ groups: FallbackTeamGroup[] }>(`${FALLBACK_BASE}/teams`),
    ]);

    // Map matches to ApiGame shape
    const games: ApiGame[] = matches.map((m) => {
      const home = m.home_team || { country: m.home_team_country || "", name: "" };
      const away = m.away_team || { country: m.away_team_country || "", name: "" };
      const kickoff = parseFallbackKickoff(m.datetime);
      const isFinished = m.status === "completed" || (m.home_team?.goals !== undefined && m.away_team?.goals !== undefined);

      return {
        id: String(m.id),
        home_team_id: home.country,
        away_team_id: away.country,
        home_score: String(home.goals ?? 0),
        away_score: String(away.goals ?? 0),
        group: m.stage_name?.includes("Group") ? m.stage_name : "",
        local_date: kickoff
          ? `${kickoff.getUTCMonth() + 1}/${kickoff.getUTCDate()}/${kickoff.getUTCFullYear()} ${kickoff.getUTCHours().toString().padStart(2, '0')}:${kickoff.getUTCMinutes().toString().padStart(2, '0')}`
          : (m.datetime || ""),
        finished: String(isFinished),
        time_elapsed: isFinished ? "FT" : (m.status === "in progress" ? "LIVE" : "NS"),
        type: m.stage_name || "Group Stage",
      };
    });

    // Map teams
    const teams: ApiTeam[] = [];
    if (teamsData.groups) {
      for (const g of teamsData.groups) {
        for (const t of g.teams) {
          teams.push({
            id: t.country,
            name_en: t.name,
            fifa_code: t.country,
            iso2: t.country,
            groups: g.letter,
          });
        }
      }
    }

    // Groups
    const groups: ApiGroup[] = teamsData.groups?.map(g => ({ name: `Group ${g.letter}` })) || [];

    console.log("✅ Using fallback provider (worldcupjson.net)");
    return { games, teams, groups, events: [], source: "fallback" };
  } catch (fallbackError) {
    console.error("❌ Fallback also failed:", fallbackError.message);
    throw new Error(`Both providers failed. Primary: ${primaryError?.message}, Fallback: ${fallbackError.message}`);
  }
}

Deno.serve(async () => {
  try {
    const { games, teams, groups, events, source } = await fetchWithFallback();

    const now = new Date();

    const activeWindow = games.some((game) => isLive(game) || isSoon(game, now));
    const next = nextKickoff(games, now);

    await supabase.schema("wc").rpc("set_sync_state", {
      p_is_live_or_soon: activeWindow,
      p_next_kickoff: next ? next.toISOString() : null,
    });

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

    const eventRows = events.map((ev) => ({
      game_id: ev.game_id ?? "",
      team_code: ev.team_code ?? ev.team_id ?? "",
      event_type: ev.type,
      minute: ev.minute ?? "",
      player: ev.player ?? "",
      canceled: ev.canceled ?? false,
      raw: ev,
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

    if (eventRows.length > 0) {
      const { error } = await supabase.schema("wc").from("events").upsert(eventRows, { onConflict: "id" });
      if (error) throw error;
    }

    return new Response(
      JSON.stringify({
        ok: true,
        source,
        live_or_soon: activeWindow,
        next_kickoff: next ? next.toISOString() : null,
        counts: {
          teams: teamRows.length,
          games: gameRows.length,
          groups: groupRows.length,
          events: eventRows.length,
        },
      }),
      { headers: { "content-type": "application/json" } },
    );
  } catch (error) {
    const message = error instanceof Error ? error.message : "Unknown error";
    return new Response(JSON.stringify({ ok: false, error: message }), {
      status: 500,
      headers: { "content-type": "application/json" },
    });
  }
});
