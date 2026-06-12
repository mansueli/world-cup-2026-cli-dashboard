create or replace function wc.sync_goal_events_from_games_raw()
returns trigger
language plpgsql
as $$
declare
  home_scorers_text text;
  away_scorers_text text;
  normalized text;
  scorer text;
  scorer_minute text;
  scorer_player text;
begin
  -- Only process when raw JSON exists
  if new.raw is null then
    return new;
  end if;

  home_scorers_text := new.raw ->> 'home_scorers';
  away_scorers_text := new.raw ->> 'away_scorers';

  -- Remove previously generated goal events for this game
  delete from wc.events e
  where e.game_id = new.game_id
    and e.event_type = 'Goal'
    and coalesce(e.raw ->> 'source', '') = 'games.raw_scorers';

  -- Parse home scorers
  if home_scorers_text is not null
     and lower(home_scorers_text) <> 'null'
     and left(trim(home_scorers_text), 1) = '{' then

    normalized := replace(replace(replace(replace(home_scorers_text, '“', '"'), '”', '"'), '{', '['), '}', ']');

    for scorer in
      select value
      from jsonb_array_elements_text(normalized::jsonb)
    loop
      scorer_minute := substring(scorer from '([0-9]{1,3}(?:\+[0-9]{1,2})?)''');
      scorer_player := trim(regexp_replace(scorer, '\s*[0-9]{1,3}(?:\+[0-9]{1,2})?''\s*$', ''));

      if coalesce(scorer_player, '') <> '' then
        insert into wc.events (game_id, team_code, event_type, minute, player, canceled, raw)
        values (
          new.game_id,
          new.home_team_id,
          'Goal',
          scorer_minute,
          scorer_player,
          false,
          jsonb_build_object(
            'source', 'games.raw_scorers',
            'side', 'home',
            'scorer_text', scorer
          )
        );
      end if;
    end loop;
  end if;

  -- Parse away scorers
  if away_scorers_text is not null
     and lower(away_scorers_text) <> 'null'
     and left(trim(away_scorers_text), 1) = '{' then

    normalized := replace(replace(replace(replace(away_scorers_text, '“', '"'), '”', '"'), '{', '['), '}', ']');

    for scorer in
      select value
      from jsonb_array_elements_text(normalized::jsonb)
    loop
      scorer_minute := substring(scorer from '([0-9]{1,3}(?:\+[0-9]{1,2})?)''');
      scorer_player := trim(regexp_replace(scorer, '\s*[0-9]{1,3}(?:\+[0-9]{1,2})?''\s*$', ''));

      if coalesce(scorer_player, '') <> '' then
        insert into wc.events (game_id, team_code, event_type, minute, player, canceled, raw)
        values (
          new.game_id,
          new.away_team_id,
          'Goal',
          scorer_minute,
          scorer_player,
          false,
          jsonb_build_object(
            'source', 'games.raw_scorers',
            'side', 'away',
            'scorer_text', scorer
          )
        );
      end if;
    end loop;
  end if;

  return new;
end;
$$;

drop trigger if exists trg_sync_goal_events_from_games_raw on wc.games;

create trigger trg_sync_goal_events_from_games_raw
after insert or update of raw on wc.games
for each row
execute function wc.sync_goal_events_from_games_raw();

alter table wc.sync_state
  add column if not exists region_cursor integer not null default 0,
  add column if not exists region_last_region text,
  add column if not exists region_last_called_at timestamp with time zone,
  add column if not exists region_last_called jsonb not null default '{}'::jsonb,
  add column if not exists region_cooldown_seconds integer not null default 120;

create or replace function wc.invoke_worldcup_sync_if_active()
returns bigint
language plpgsql
security definer
as $$
declare
  cfg wc.automation_config%rowtype;
  live_or_soon boolean;
  cursor integer;
  cooldown_seconds integer;
  last_called jsonb;

  regions text[] := array[
    'ap-northeast-1','ap-northeast-2','ap-south-1','ap-southeast-1','ap-southeast-2',
    'ca-central-1','us-east-1','us-west-1','us-west-2',
    'eu-central-1','eu-west-1','eu-west-2','eu-west-3',
    'sa-east-1'
  ];
  region_count integer;
  i integer;
  idx integer;
  candidate_region text;
  candidate_last_called_at timestamp with time zone;
  selected_region text;

  req_id bigint;
begin
  -- Lock singleton state row so concurrent invocations don't select the same region.
  select
    s.is_live_or_soon,
    coalesce(s.region_cursor, 0),
    coalesce(s.region_last_called, '{}'::jsonb),
    greatest(coalesce(s.region_cooldown_seconds, 120), 0)
  into
    live_or_soon,
    cursor,
    last_called,
    cooldown_seconds
  from wc.sync_state s
  where s.singleton = true
  for update;

  if not found then
    raise exception 'wc.sync_state is not configured';
  end if;

  if coalesce(live_or_soon, false) = false then
    return null;
  end if;

  select * into cfg
  from wc.automation_config
  where singleton = true;

  if not found then
    raise exception 'wc.automation_config is not configured';
  end if;

  region_count := coalesce(array_length(regions, 1), 0);
  if region_count = 0 then
    raise exception 'No regions configured for worldcup sync';
  end if;

  -- Round-robin through regions, picking the first outside cooldown.
  for i in 0..region_count - 1 loop
    idx := ((cursor + i) % region_count) + 1;
    candidate_region := regions[idx];

    candidate_last_called_at := nullif(last_called ->> candidate_region, '')::timestamptz;

    if candidate_last_called_at is null
       or now() - candidate_last_called_at >= make_interval(secs => cooldown_seconds) then
      selected_region := candidate_region;
      cursor := idx;
      exit;
    end if;
  end loop;

  -- If every region is still in cooldown, skip this cycle.
  if selected_region is null then
    return null;
  end if;

  select net.http_post(
    url := cfg.function_url,
    headers := jsonb_build_object(
      'Content-Type', 'application/json',
      'Authorization', 'Bearer ' || cfg.auth_token,
      'x-region', selected_region
    ),
    body := '{}'::jsonb
  ) into req_id;

  update wc.sync_state
  set
    region_cursor = cursor,
    region_last_region = selected_region,
    region_last_called_at = now(),
    region_last_called = jsonb_set(
      coalesce(region_last_called, '{}'::jsonb),
      array[selected_region],
      to_jsonb(now()),
      true
    ),
    updated_at = now()
  where singleton = true;

  return req_id;
end;
$$;