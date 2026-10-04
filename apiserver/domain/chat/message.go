package chat

const EventTurnStart = "turn_start"
const EventDone = "done"

type Request struct {
	Text string `json:"text"`
}

type MessageMetadata struct {
	MessageID   string `json:"message_id"`
	AssistantID string `json:"assistant_id"`
}
