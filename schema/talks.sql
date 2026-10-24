create table talks (
  id uuid primary key default gen_random_uuid(),
  title text not null,
  description text not null,
  date timestamp not null,
  duration interval not null,
  location text not null,
  language text not null default 'es',
  stream_url text,
  poster_url text,
  published boolean not null default false,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),

  unique(title, date, location),
  constraint talks_title_not_blank check (trim(title) <> ''),
  constraint talks_description_not_blank check (trim(description) <> ''),
  constraint talks_duration_positive check (duration > interval '0'),
  constraint talks_location_not_blank check (trim(location) <> ''),
  constraint talks_language_valid check (language in ('es', 'en')),
  constraint talks_stream_url_valid check (is_valid_url(stream_url)),
  constraint talks_poster_url_valid check (is_valid_url(poster_url))
);

create table talk_speakers (
  talk_id uuid not null references talks(id) on delete cascade,
  person_id uuid not null references people(id),

  primary key (talk_id, person_id)
);

create constraint trigger talks_require_speaker
  after insert on talks
  deferrable initially deferred
  for each row execute function talks_require_speaker();

create constraint trigger talk_speakers_require_speaker
  after delete or update of talk_id on talk_speakers
  deferrable initially deferred
  for each row execute function talks_require_speaker();

create trigger talks_set_updated_at
  before update on talks
  for each row execute function set_updated_at();
