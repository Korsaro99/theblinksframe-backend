--liquibase formatted sql

--changeset blinks:0003-staff-users
--comment: Back-office accounts. Passwords are bcrypt hashes. Bumping token_version invalidates every access JWT at once.
CREATE TABLE staff_users (
  id            uuid        PRIMARY KEY DEFAULT uuidv7(),
  name          text        NOT NULL CHECK (btrim(name) <> ''),
  phone         text        NOT NULL UNIQUE CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'),
  email         text,
  password_hash text        NOT NULL,
  role          text        NOT NULL CHECK (role IN ('OWNER', 'STAFF')),
  is_active     boolean     NOT NULL DEFAULT true,
  token_version integer     NOT NULL DEFAULT 0,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX staff_users_email_lower_key ON staff_users (lower(email)) WHERE email IS NOT NULL;
CREATE TRIGGER staff_users_set_updated_at BEFORE UPDATE ON staff_users
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
--rollback DROP TABLE staff_users;

--changeset blinks:0003-auth-sessions
--comment: One row per login (a refresh-token family). Revoking it logs that device out.
CREATE TABLE auth_sessions (
  id                  uuid        PRIMARY KEY DEFAULT uuidv7(),
  staff_user_id       uuid        NOT NULL REFERENCES staff_users (id) ON DELETE CASCADE,
  created_at          timestamptz NOT NULL DEFAULT now(),
  last_used_at        timestamptz NOT NULL DEFAULT now(),
  idle_expires_at     timestamptz NOT NULL,
  absolute_expires_at timestamptz NOT NULL,
  revoked_at          timestamptz,
  revoke_reason       text CHECK (revoke_reason IN ('LOGOUT', 'REUSE_DETECTED', 'ADMIN', 'PASSWORD_CHANGE')),
  user_agent          text        NOT NULL DEFAULT '',
  ip                  inet,
  CONSTRAINT auth_sessions_revoked_has_reason CHECK ((revoked_at IS NULL) = (revoke_reason IS NULL))
);
CREATE INDEX auth_sessions_staff_user_id_idx ON auth_sessions (staff_user_id) WHERE revoked_at IS NULL;
--rollback DROP TABLE auth_sessions;

--changeset blinks:0003-refresh-tokens
--comment: Rotated refresh tokens. Only a SHA-256 hash is stored. Presenting a used token revokes its whole session.
CREATE TABLE refresh_tokens (
  id             uuid        PRIMARY KEY DEFAULT uuidv7(),
  session_id     uuid        NOT NULL REFERENCES auth_sessions (id) ON DELETE CASCADE,
  token_hash     bytea       NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
  issued_at      timestamptz NOT NULL DEFAULT now(),
  used_at        timestamptz,
  replaced_by_id uuid        REFERENCES refresh_tokens (id) ON DELETE SET NULL
);
CREATE INDEX refresh_tokens_session_id_idx ON refresh_tokens (session_id);
--rollback DROP TABLE refresh_tokens;
