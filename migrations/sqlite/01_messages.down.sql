DROP INDEX IF EXISTS idx_message_summaries_conversation;

--bun:split

DROP TABLE IF EXISTS message_summaries;

--bun:split

DROP INDEX IF EXISTS idx_messages_conversation_turn;

--bun:split

DROP TABLE IF EXISTS messages;
