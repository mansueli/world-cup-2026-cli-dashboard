create extension if not exists pg_net;
create extension if not exists pg_cron;

do $$
begin
  if to_regclass('wc.sync_state') is null then
    raise exception 'Missing wc.sync_state. Apply 20260609_worldcup_sync.sql before 20260609_worldcup_cron_automation.sql.';
  end if;
end;
$$;

create table if not exists wc.automation_config (
  singleton boolean primary key default true,
  function_url text not null,
  auth_token text not null,
  updated_at timestamptz not null default now(),
  check (singleton)
);

create or replace function wc.configure_worldcup_sync(
  p_function_url text,
  p_auth_token text
) returns void
language plpgsql
security definer
as $$
begin
  if trim(coalesce(p_function_url, '')) = '' then
    raise exception 'p_function_url cannot be empty';
  end if;

  if trim(coalesce(p_auth_token, '')) = '' then
    raise exception 'p_auth_token cannot be empty';
  end if;

  insert into wc.automation_config (singleton, function_url, auth_token, updated_at)
  values (true, trim(p_function_url), trim(p_auth_token), now())
  on conflict (singleton)
  do update
  set function_url = excluded.function_url,
      auth_token = excluded.auth_token,
      updated_at = now();
end;
$$;

create or replace function wc.invoke_worldcup_sync()
returns bigint
language plpgsql
security definer
as $$
declare
  cfg wc.automation_config%rowtype;
  req_id bigint;
begin
  select * into cfg
  from wc.automation_config
  where singleton = true;

  if not found then
    raise exception 'wc.automation_config is not configured';
  end if;

  select net.http_post(
    url := cfg.function_url,
    headers := jsonb_build_object(
      'Content-Type', 'application/json',
      'Authorization', 'Bearer ' || cfg.auth_token
    ),
    body := '{}'::jsonb
  ) into req_id;

  return req_id;
end;
$$;

create or replace function wc.invoke_worldcup_sync_if_active()
returns bigint
language plpgsql
security definer
as $$
declare
  live_or_soon boolean;
begin
  select is_live_or_soon
  into live_or_soon
  from wc.sync_state
  where singleton = true;

  if coalesce(live_or_soon, false) = false then
    return null;
  end if;

  return wc.invoke_worldcup_sync();
end;
$$;

create or replace function wc.unschedule_worldcup_sync_jobs()
returns void
language plpgsql
security definer
as $$
declare
  job record;
begin
  for job in
    select jobname
    from cron.job
    where jobname in ('worldcup-sync-heartbeat', 'worldcup-sync-live-secondly')
  loop
    perform cron.unschedule(job.jobname);
  end loop;
end;
$$;

create or replace function wc.schedule_worldcup_sync_jobs()
returns void
language plpgsql
security definer
as $$
begin
  perform wc.unschedule_worldcup_sync_jobs();

  -- Always run every minute to keep next kickoff and state fresh.
  perform cron.schedule(
    'worldcup-sync-heartbeat',
    '* * * * *',
    $$select wc.invoke_worldcup_sync();$$
  );

  -- Run every second only while live_or_soon=true.
  perform cron.schedule(
    'worldcup-sync-live-secondly',
    '1 second',
    $$select wc.invoke_worldcup_sync_if_active();$$
  );
end;
$$;

alter table wc.automation_config enable row level security;

drop policy if exists "No public reads automation config" on wc.automation_config;
create policy "No public reads automation config"
on wc.automation_config for select
using (false);
