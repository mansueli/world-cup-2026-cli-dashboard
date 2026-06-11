create unique index if not exists uniq_events_game_player_minute_type 
  on wc.events(game_id, player, minute, event_type) where canceled = false;