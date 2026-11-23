CREATE TABLE IF NOT EXISTS users (
user_id UUID Primary Key DEFAULT gen_random_uuid(), /* REMOVE DEFAULT AND RAISE EXCEPTION IF its not passed.*/
player_id varchar(20) Unique NOT NULL,
last_login TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
