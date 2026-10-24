create table sessions (
  token_hash bytea primary key,
  person_id uuid not null references people(id) on delete cascade,
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),

  constraint sessions_token_hash_sha256 check (octet_length(token_hash) = 32),
  constraint sessions_expires_after_created check (expires_at > created_at)
);

create index sessions_person_id_idx on sessions (person_id);
