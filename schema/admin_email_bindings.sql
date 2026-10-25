create table admin_email_bindings (
  email_hash bytea primary key,
  constraint admin_email_bindings_hash_sha256 check (octet_length(email_hash) = 32)
);
