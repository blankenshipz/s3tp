CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE public.events
(
   id            uuid         NOT NULL DEFAULT uuid_generate_v1()
  ,session_id    uuid         NOT NULL
  ,access_key_id varchar(20)  NOT NULL
  ,type          varchar(8)   NOT NULL
  ,size          bigint       NOT NULL DEFAULT 0
  ,created_at    timestamptz  NOT NULL DEFAULT NOW()
  ,updated_at    timestamptz  NOT NULL DEFAULT NOW()
);
