create type person_role as enum ('member', 'admin');

create table people (
  id uuid primary key default gen_random_uuid(),
  first_name text not null,
  last_name text not null,
  email text unique,
  google_sub text unique,
  role person_role not null default 'member',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),

  constraint people_first_name_not_blank check (trim(first_name) <> ''),
  constraint people_last_name_not_blank check (trim(last_name) <> ''),
  constraint people_email_canonical check (is_canonical_email(email)),
  constraint people_google_sub_not_blank check (trim(google_sub) <> ''),
  constraint people_google_sub_requires_email check (google_sub is null or email is not null)
);

create trigger people_set_updated_at
  before update on people
  for each row execute function set_updated_at();
