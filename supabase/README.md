# Supabase Setup

This folder contains database and edge-function scaffolding for event-driven World Cup updates.

## Files

- `migrations/20260609_worldcup_sync.sql`: creates `wc` schema, cache tables, sync state, and broadcast trigger.
- `migrations/20260609_worldcup_cron_automation.sql`: enables `pg_net` + `pg_cron` and adds automated sync schedulers.
- `functions/worldcup-sync/index.ts`: syncs upstream API into Supabase and controls broadcast window.

## Deploy

1. Apply migration:

```bash
supabase db push
```

2. Deploy edge function:

```bash
supabase functions deploy worldcup-sync
```

3. Set edge function secrets:

```bash
supabase secrets set WC_API_BASE=https://worldcup26.ir
supabase secrets set WC_WINDOW_MINUTES=15
supabase secrets set SUPABASE_URL=<your-project-url>
supabase secrets set SUPABASE_SERVICE_ROLE_KEY=<service-role-key>
```

4. Invoke function from scheduler.

Recommended behavior:
- invoke every second only while `wc.sync_state.is_live_or_soon = true`
- otherwise invoke less frequently (for example every 60 seconds) to refresh `next_kickoff`

## Full automation with pg_net + pg_cron

After applying migrations, configure the edge function endpoint and token in SQL:

```sql
select wc.configure_worldcup_sync(
	'https://<project-ref>.functions.supabase.co/worldcup-sync',
	'<service-role-or-function-token>'
);
```

Then install schedules:

```sql
select wc.schedule_worldcup_sync_jobs();
```

What gets scheduled:

- `worldcup-sync-heartbeat`: every minute (`* * * * *`) to keep state warm.
- `worldcup-sync-live-secondly`: every second (`1 second`) but only dispatches HTTP calls when `wc.sync_state.is_live_or_soon = true`.

To remove schedules:

```sql
select wc.unschedule_worldcup_sync_jobs();
```

## Client connection policy

Before subscribing to realtime:

1. Read `wc.sync_state.is_live_or_soon`.
2. Subscribe only when true.
3. Unsubscribe when false.

This keeps realtime connections closed outside live or pre-live windows.
