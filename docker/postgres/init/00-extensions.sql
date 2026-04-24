-- Enable extensions needed by the app. Migrations also ensure these exist,
-- but enabling here means the first connection already has them available.
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS postgis_raster;  -- required dependency of h3_postgis
CREATE EXTENSION IF NOT EXISTS h3;
CREATE EXTENSION IF NOT EXISTS h3_postgis;
