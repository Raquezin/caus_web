\set ON_ERROR_STOP on
begin;
\ir schema/functions.sql
\ir schema/people.sql
\ir schema/auth.sql
\ir schema/admin_email_bindings.sql
\ir schema/talks.sql
\ir schema/competitions.sql
commit;
