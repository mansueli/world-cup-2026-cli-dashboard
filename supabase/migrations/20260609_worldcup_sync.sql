create schema if not exists wc;

create table if not exists wc.sync_state (
  singleton boolean primary key default true,
  is_live_or_soon boolean not null default false,
  next_kickoff timestamptz,
  updated_at timestamptz not null default now(),
  check (singleton)
);

insert into wc.sync_state (singleton, is_live_or_soon)
values (true, false)
on conflict (singleton) do nothing;

create table if not exists wc.teams (
  team_id text primary key,
  fifa_code text,
  iso2 text,
  group_name text,
  name_en text,
  raw jsonb not null,
  updated_at timestamptz not null default now()
);

create table if not exists wc.games (
  game_id text primary key,
  home_team_id text,
  away_team_id text,
  group_name text,
  stage text,
  finished boolean not null default false,
  time_elapsed text,
  local_date_raw text,
  kickoff_at timestamptz,
  home_score integer not null default 0,
  away_score integer not null default 0,
  raw jsonb not null,
  updated_at timestamptz not null default now()
);

create index if not exists games_kickoff_at_idx on wc.games (kickoff_at);
create index if not exists games_finished_idx on wc.games (finished);

create table if not exists wc.groups (
  group_name text primary key,
  raw jsonb not null,
  updated_at timestamptz not null default now()
);

create or replace function wc.set_sync_state(
  p_is_live_or_soon boolean,
  p_next_kickoff timestamptz
) returns void
language plpgsql
security definer
as $$
begin
  insert into wc.sync_state (singleton, is_live_or_soon, next_kickoff, updated_at)
  values (true, p_is_live_or_soon, p_next_kickoff, now())
  on conflict (singleton)
  do update
  set is_live_or_soon = excluded.is_live_or_soon,
      next_kickoff = excluded.next_kickoff,
      updated_at = now();
end;
$$;

create or replace function wc.broadcast_game_change()
returns trigger
language plpgsql
security definer
as $$
declare
  should_emit boolean;
begin
  select is_live_or_soon
  into should_emit
  from wc.sync_state
  where singleton = true;

  if coalesce(should_emit, false) = false then
    if tg_op = 'DELETE' then
      return old;
    end if;
    return new;
  end if;

  perform realtime.broadcast_changes(
    'worldcup-live',
    tg_op,
    tg_op,
    tg_table_name,
    tg_table_schema,
    new,
    old
  );

  if tg_op = 'DELETE' then
    return old;
  end if;

  return new;
end;
$$;

drop trigger if exists worldcup_games_broadcast_trigger on wc.games;

create trigger worldcup_games_broadcast_trigger
after insert or update or delete on wc.games
for each row execute procedure wc.broadcast_game_change();

alter table wc.teams enable row level security;
alter table wc.games enable row level security;
alter table wc.groups enable row level security;
alter table wc.sync_state enable row level security;

drop policy if exists "Public can read teams" on wc.teams;
create policy "Public can read teams"
on wc.teams for select
using (true);

drop policy if exists "Public can read games" on wc.games;
create policy "Public can read games"
on wc.games for select
using (true);

drop policy if exists "Public can read groups" on wc.groups;
create policy "Public can read groups"
on wc.groups for select
using (true);

drop policy if exists "Public can read sync state" on wc.sync_state;
create policy "Public can read sync state"
on wc.sync_state for select
using (true);
