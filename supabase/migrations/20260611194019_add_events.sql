-- Migration: Add events table for match events (goals, yellow cards, red cards, substitutions, etc.)
-- Run after 20260609_worldcup_sync.sql

create schema if not exists wc;

create table if not exists wc.events (
    id bigserial primary key,
    game_id text not null references wc.games(game_id) on delete cascade,
    team_code text,                    -- normalized 3-letter code (e.g. BRA, ARG)
    event_type text not null,          -- "Goal", "Yellow Card", "Second Yellow Card", "Red Card", "Substitution In", etc.
    minute text,
    player text,
    canceled boolean default false,
    raw jsonb,                         -- original event object from upstream for debugging/flexibility
    created_at timestamptz default now(),
    updated_at timestamptz default now()
);
-- Enable Row-Level Security and create a policy to allow read access to all users (adjust as needed)
alter table wc.events enable row level security;
create policy "Allow read access to events" on wc.events for select using (true);

-- Helpful indexes
create index if not exists idx_events_game_id on wc.events(game_id);
create index if not exists idx_events_team_code on wc.events(team_code);
create index if not exists idx_events_type on wc.events(event_type);
create index if not exists idx_events_game_type on wc.events(game_id, event_type);

-- Optional: unique constraint to avoid duplicates on re-sync (adjust if needed)
-- create unique index if not exists uniq_events_game_player_minute_type 
--   on wc.events(game_id, player, minute, event_type) where canceled = false;

comment on table wc.events is 'Match events (goals, cards, subs) synced from upstream World Cup API';
comment on column wc.events.event_type is 'Matches data.EventType constants in the Go CLI (Goal, Yellow Card, Second Yellow Card, Red Card, etc.)';