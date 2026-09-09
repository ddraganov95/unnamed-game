CREATE OR REPLACE FUNCTION global_chat_message_send(
p_user_id UUID,
p_message VARCHAR(70),
p_broadcast VARCHAR(100)
)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN

INSERT INTO global_chat_messages (user_id, message)
VALUES (p_user_id, p_message);

PERFORM pg_notify('global_chat',p_broadcast);

END;
$$;