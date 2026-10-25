create function is_canonical_email(value text)
returns boolean
language sql
immutable
strict
parallel safe
as $$
  select value = lower(trim(value))
    and value ~ '^[^[:space:]@]+@[^[:space:]@]+[.][^[:space:]@]+$';
$$;

create function is_valid_url(value text)
returns boolean
language sql
immutable
strict
parallel safe
as $$
  with parsed as (
    select regexp_match(value, '^https?://(\[[0-9a-f:.]+\]|[a-z0-9.-]+)(?::([0-9]+))?([/?#][^[:space:][:cntrl:]\\]*)?$', 'i') as parts
  )
  select coalesce(
    case
      when left(parts[1], 1) = '[' then
        strpos(parts[1], ':') > 0 and pg_input_is_valid(substr(parts[1], 2, length(parts[1]) - 2), 'inet')
      when parts[1] ~ '^[0-9.]+$' then
        parts[1] ~ '^([0-9]{1,3}\.){3}[0-9]{1,3}$' and pg_input_is_valid(parts[1], 'inet')
      else
        length(rtrim(parts[1], '.')) <= 253 and parts[1] ~* '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*\.?$'
    end
    and case
      when parts[2] is null then true
      when length(ltrim(parts[2], '0')) between 1 and 5 then ltrim(parts[2], '0')::integer <= 65535
      else false
    end
    and strpos(regexp_replace(value, '%[0-9a-fA-F]{2}', '', 'g'), '%') = 0,
    false
  ) from parsed;
$$;

create function set_updated_at()
returns trigger
language plpgsql
as $$
begin
  new.updated_at := now();
  return new;
end;
$$;

create function talks_require_speaker()
returns trigger
language plpgsql
as $$
declare
  checked_talk_id uuid;
begin
  if tg_table_name = 'talks' then
    checked_talk_id := new.id;
  else
    checked_talk_id := old.talk_id;
  end if;

  perform from talks where id = checked_talk_id for no key update;

  if found and not exists (select 0 from talk_speakers where talk_id = checked_talk_id) then
    raise exception 'talk % must have at least one speaker', checked_talk_id
      using errcode = 'check_violation';
  end if;

  return null;
end;
$$;

create function teams_require_members()
returns trigger
language plpgsql
as $$
declare
  team_ids uuid[];
  checked_team_id uuid;
  checked_team_size smallint;
begin
  if tg_table_name = 'competitions' then
    team_ids := array(select id from teams where competition_id = new.id);
  elsif tg_table_name = 'teams' then
    team_ids := array[new.id];
  elsif tg_op = 'INSERT' then
    team_ids := array[new.team_id];
  elsif tg_op = 'DELETE' then
    team_ids := array[old.team_id];
  else
    team_ids := array[old.team_id, new.team_id];
  end if;

  foreach checked_team_id in array team_ids loop
    perform from competitions
      where id = (select competition_id from teams where id = checked_team_id)
      for share;
    perform from teams where id = checked_team_id for no key update;
    continue when not found;

    select team_size into checked_team_size
      from teams
      join competitions on competitions.id = teams.competition_id
      where teams.id = checked_team_id
      for share of competitions;

    if (select count(*) from team_members where team_id = checked_team_id) not between 1 and checked_team_size then
      raise exception 'team % must have between 1 and % members', checked_team_id, checked_team_size
        using errcode = 'check_violation';
    end if;
  end loop;

  return null;
end;
$$;
