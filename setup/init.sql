-- future_offerings/scripts/init.sql
-- Create app database
CREATE DATABASE future_offerings_db;

-- Create (or reuse) application role
DO $$
BEGIN
   BEGIN
      CREATE ROLE superu WITH LOGIN PASSWORD 'Agb3zud0r';
   EXCEPTION WHEN DUPLICATE_OBJECT THEN
      RAISE NOTICE 'Role superu already exists, skipping creation';
   END;
END $$;

-- Give the role database-level privileges
GRANT ALL PRIVILEGES ON DATABASE future_offerings_db TO superu;

-- ▶️ Switch to the new database so the next GRANTs apply to its public schema
\connect future_offerings_db

-- Grant schema-level privileges and set sane defaults for future tables
GRANT ALL PRIVILEGES ON SCHEMA public TO superu;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO superu;