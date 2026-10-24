# CAUS BACKEND SCHEMA

`schema.sql` is the source of truth. This file only explains the rules behind it.

Recurring things (talks, competitions, results) live in the DB. One-off news (Advent of Code,
elections, chronicles) stays as Markdown in Git and is not part of this schema.

## People

- Members, speakers and competitors.
- First and last name are required. Names can be corrected by the person even if they come from Google.
- Email is optional: deleting an account clears it, but the row stays so talks and results keep their history.
- Roles: `member` (default) and `admin`.

## Auth

- Sign in with Google only. `google_sub` links a person to their Google account and requires an email.
- On first sign-in, a person with no `google_sub` but the same verified email (e.g. a speaker created by an admin) is linked instead of duplicated.
- Sessions store only `sha256(token)`; the token itself lives in the cookie.
- Deleting an account clears `email` and `google_sub` and removes its sessions; the person row stays.

## Talks

- Every talk has at least one speaker, and speakers are people (they must register).
- Dates are Seville local time (`timestamp`), with a `duration`.
- Language is `es` (default) or `en`.
- Talks start unpublished and only appear on the site once `published`.
- Series such as the maths talks are a query, not a column.

## Competitions

- Own competitions (CompliCAUS) and external ones (AdaByron). For external ones only the US teams are stored.
- `description` is Markdown: announcement, prizes and any extra text.
- Every participation is a team. `team_size` is 3 by default and 1 for individual competitions.
- A team has between 1 and `team_size` members; smaller teams are the rare exceptions.
- A person can be in only one team per competition.
- Individual entries need no team name.
- `position` is set after the competition (for external ones, the position in the overall ranking).
