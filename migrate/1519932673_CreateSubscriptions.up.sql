CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE subscriptions(
   id            uuid        NOT NULL DEFAULT uuid_generate_v1()
  ,heorku_id     uuid        NOT NULL
  ,plan_id       uuid        NOT NULL
  ,region        text        NOT NULL
  ,access_key_id varchar(20) NOT NULL
  ,heroku_app_id text        NOT NULL
  ,active        boolean     NOT NULL DEFAULT TRUE
  ,created_at    timestamptz NOT NULL DEFAULT NOW()
  ,updated_at    timestamptz NOT NULL DEFAULT NOW()
);
