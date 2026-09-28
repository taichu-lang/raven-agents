package chat

const EventTurnStart = "turn_start"
const EventDone = "done"

type Request struct {
	Text string `json:"text"`
}

type MessageMetadata struct {
	ID string `json:"id"`
}
