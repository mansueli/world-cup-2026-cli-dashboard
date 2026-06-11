create table if not exists wc.events (
    id bigserial primary key,
    game_id text not null references wc.games(game_id) on delete cascade,
    team_code text,
    event_type text not null,
    minute text,
    player text,
    canceled boolean default false,
    raw jsonb,
    created_at timestamptz default now(),
    updated_at timestamptz default now()
);

alter table wc.events enable row level security;
create policy "Allow read access to events" on wc.events for select using (true);

create index if not exists idx_events_game_id on wc.events(game_id);
create index if not exists idx_events_team_code on wc.events(team_code);
create index if not exists idx_events_type on wc.events(event_type);
create index if not exists idx_events_game_type on wc.events(game_id, event_type);

comment on table wc.events is 'Match events (goals, cards, subs) synced from upstream World Cup API';
comment on column wc.events.event_type is 'Matches data.EventType constants in the Go CLI (Goal, Yellow Card, Second Yellow Card, Red Card, etc.)';