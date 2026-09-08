-- Create auth database for the Auth Service
CREATE DATABASE auth_db;

-- Grant privileges to the main postgres user
GRANT ALL PRIVILEGES ON DATABASE auth_db TO smartgrid;
