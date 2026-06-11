// @ts-nocheck
// Minimal, safe addition of event syncing on top of the existing production edge function.
// Only the event-related parts were added/changed. Everything else is preserved from the live version.

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
  // Events may be present in the raw payload from worldcup26.ir
  events?: any[];
  home_events?: any[];
  away_events?: any[];
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
};

type FallbackTeamGroup = {
  letter: string;
  teams: Array<{
    country: string;
    name: string;
    group_letter?: string;
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
    const [gamesPayload, teamsPayload, groupsPayload] = await Promise.all([
      fetchJSON<{ games: ApiGame[] }>(`${API_BASE}/get/games`),
      fetchJSON<{ teams: ApiTeam[] }>(`${API_BASE}/get/teams`),
      fetchJSON<{ groups: ApiGroup[] }>(`${API_BASE}/get/groups`),
    ]);

    console.log("✅ Using primary provider (worldcup26.ir)");
    return {
      games: gamesPayload.games ?? [],
      teams: teamsPayload.teams ?? [],
      groups: groupsPayload.groups ?? [],
      source: "primary",
    };
  } catch (primaryError) {
    console.error("⚠️ Primary provider failed:", primaryError.message);
    console.log("🔄 Falling back to worldcupjson.net...");
  }

  try {
    const [matches, teamsData] = await Promise.all([
      fetchJSON<FallbackGame[]>(`${FALLBACK_BASE}/matches`),
      fetchJSON<{ groups: FallbackTeamGroup[] }>(`${FALLBACK_BASE}/teams`),
    ]);

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

    const groups: ApiGroup[] = teamsData.groups?.map(g => ({ name: `Group ${g.letter}` })) || [];

    console.log("✅ Using fallback provider (worldcupjson.net)");
    return { games, teams, groups, source: "fallback" };
  } catch (fallbackError: any) {
    console.error("❌ Fallback also failed:", fallbackError.message);
    throw new Error(`Both providers failed. Primary: ${primaryError?.message}, Fallback: ${fallbackError.message}`);
  }
}

// === NEW: Minimal event syncing (added safely) ===

type NormalizedEvent = {
  event_type: string;
  minute: string;
  player: string;
  canceled: boolean;
  team_code: string | null;
};

function normalizeEvent(e: any, game?: any, forcedSide?: "home" | "away"): NormalizedEvent | null {
  if (!e) return null;

  let eventType = String(e.type || e.event_type || e.event || "").trim();
  const minute = String(e.minute ?? e.time ?? e.min ?? "").trim();
  const player = String(e.player ?? e.player_name ?? e.name ?? "").trim();
  const canceled = e.canceled === true || e.cancelled === true;

  if (!eventType || !player) return null;

  const typeLower = eventType.toLowerCase();
  if (typeLower.includes("yellow")) {
    eventType = typeLower.includes("second") ? "Second Yellow Card" : "Yellow Card";
  } else if (typeLower.includes("red")) {
    eventType = "Red Card";
  } else if (typeLower.includes("goal")) {
    if (typeLower.includes("penalty") || typeLower.includes("(p)")) eventType = "Goal (P)";
    else if (typeLower.includes("own")) eventType = "Own Goal";
    else eventType = "Goal";
  } else if (typeLower.includes("sub")) {
    eventType = typeLower.includes("in") ? "Substitution In" : "Substitution Out";
  }

  let teamCode = e.team_code || e.team || e.side || "";
  if (!teamCode && forcedSide && game) {
    teamCode = forcedSide === "home"
      ? (game.home_team_id || game.home_team_country || "")
      : (game.away_team_id || game.away_team_country || "");
  }

  return {
    event_type: eventType,
    minute,
    player,
    canceled,
    team_code: teamCode ? String(teamCode).toUpperCase().slice(0, 3) : null,
  };
}

async function syncEventsForGame(gameId: string, rawGame: any) {
  const eventsToInsert: any[] = [];

  const rawEvents =
    rawGame.events ||
    rawGame.home_events ||
    rawGame.away_events ||
    rawGame.raw?.events ||
    rawGame.raw?.home_events ||
    rawGame.raw?.away_events ||
    [];

  if (Array.isArray(rawEvents) && rawEvents.length > 0) {
    for (const e of rawEvents) {
      const normalized = normalizeEvent(e, rawGame);
      if (normalized) eventsToInsert.push({ game_id: gameId, ...normalized, raw: e });
    }
  } else if (rawGame.home_events && rawGame.away_events) {
    for (const e of rawGame.home_events) {
      const normalized = normalizeEvent(e, rawGame, "home");
      if (normalized) eventsToInsert.push({ game_id: gameId, ...normalized, raw: e });
    }
    for (const e of rawGame.away_events) {
      const normalized = normalizeEvent(e, rawGame, "away");
      if (normalized) eventsToInsert.push({ game_id: gameId, ...normalized, raw: e });
    }
  }

  if (eventsToInsert.length === 0) return;

  try {
    const { error } = await supabase.schema("wc").from("events").upsert(eventsToInsert, {
      onConflict: "game_id,player,minute,event_type",
      ignoreDuplicates: false,
    });
    if (error) {
      console.warn(`Event upsert warning for game ${gameId}:`, error.message);
    }
  } catch (err) {
    console.warn(`Event sync failed for game ${gameId}:`, err);
  }
}

// === End of added event code ===

Deno.serve(async () => {
  try {
    const { games, teams, groups, source } = await fetchWithFallback();

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

    if (teamRows.length > 0) {
      const { error } = await supabase.schema("wc").from("teams").upsert(teamRows, { onConflict: "team_id" });
      if (error) throw error;
    }

    if (gameRows.length > 0) {
      const { error } = await supabase.schema("wc").from("games").upsert(gameRows, { onConflict: "game_id" });
      if (error) throw error;

      // NEW: Sync events after games are safely upserted
      for (const game of games) {
        await syncEventsForGame(String(game.id), game);
      }
    }

    if (groupRows.length > 0) {
      const { error } = await supabase.schema("wc").from("groups").upsert(groupRows, { onConflict: "group_name" });
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
