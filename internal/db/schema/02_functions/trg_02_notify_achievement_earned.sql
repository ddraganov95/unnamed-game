CREATE OR REPLACE FUNCTION trg_02_notify_achievement_earned()
RETURNS TRIGGER AS $$
DECLARE
    v_payload TEXT;
BEGIN
    SELECT json_build_object(
        'player_id', u.player_id,
        'title', a.title,
        'is_global_announcement', a.is_global_announcement
    )::text
    INTO v_payload
    FROM users u, achievements a
    WHERE u.user_id = NEW.user_id
      AND a.achievement_id = NEW.achievement_id;

    PERFORM pg_notify('achievement_unlocked', v_payload);

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;