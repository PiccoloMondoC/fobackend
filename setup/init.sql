DO $$
BEGIN
   BEGIN
      CREATE ROLE superu WITH LOGIN PASSWORD 'Agb3zud0r';
   EXCEPTION WHEN DUPLICATE_OBJECT THEN
      RAISE NOTICE 'Role superu already exists, skipping creation';
   END;
END $$;

GRANT ALL PRIVILEGES ON DATABASE sd_deals_db TO superu;

ALTER DATABASE sd_deals_db SET timezone TO 'UTC';

\connect sd_deals_db

GRANT ALL PRIVILEGES ON SCHEMA public TO superu;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO superu;