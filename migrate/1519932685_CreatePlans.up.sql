CREATE TABLE plans (
   id          uuid          NOT NULL DEFAULT uuid_generate_v1()
  ,description text          NOT NULL
  ,cost        numeric(8, 2) NOT NULL
  ,created_at  timestamptz   NOT NULL DEFAULT NOW()
  ,updated_at  timestamptz   NOT NULL DEFAULT NOW()
);
