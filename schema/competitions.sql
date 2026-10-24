create table competitions (
  id uuid primary key default gen_random_uuid(),
  name text not null,
  description text not null,
  date timestamp not null,
  location text not null,
  team_size smallint not null default 3,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),

  unique(name, date),
  constraint competitions_name_not_blank check (trim(name) <> ''),
  constraint competitions_description_not_blank check (trim(description) <> ''),
  constraint competitions_location_not_blank check (trim(location) <> ''),
  constraint competitions_team_size_positive check (team_size >= 1)
);

create table teams (
  id uuid primary key default gen_random_uuid(),
  competition_id uuid not null references competitions(id) on delete cascade,
  name text,
  position smallint,
  photo_url text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),

  unique (id, competition_id),
  unique (competition_id, name),
  constraint teams_name_not_blank check (trim(name) <> ''),
  constraint teams_position_positive check (position >= 1),
  constraint teams_photo_url_valid check (is_valid_url(photo_url))
);

create table team_members (
  team_id uuid not null,
  competition_id uuid not null,
  person_id uuid not null references people(id),

  primary key (team_id, person_id),
  unique (competition_id, person_id),
  foreign key (team_id, competition_id)
    references teams(id, competition_id) on delete cascade
);

create constraint trigger teams_require_members
  after insert on teams
  deferrable initially deferred
  for each row execute function teams_require_members();

create constraint trigger team_members_require_members
  after insert or delete or update of team_id on team_members
  deferrable initially deferred
  for each row execute function teams_require_members();

create constraint trigger competitions_require_team_size
  after update of team_size on competitions
  deferrable initially deferred
  for each row execute function teams_require_members();

create trigger competitions_set_updated_at
  before update on competitions
  for each row execute function set_updated_at();

create trigger teams_set_updated_at
  before update on teams
  for each row execute function set_updated_at();
