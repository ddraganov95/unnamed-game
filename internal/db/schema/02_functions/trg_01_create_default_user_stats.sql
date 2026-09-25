CREATE OR REPLACE FUNCTION trg_01_create_default_user_stats()
RETURNS TRIGGER AS $$
BEGIN
INSERT INTO user_stats(user_id)
VALUES (NEW.user_id)
ON CONFLICT (user_id) DO NOTHING;
RETURN NEW;
END;
$$
Language plpgsql;