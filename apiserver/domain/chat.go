package domain

type ChatRequest struct {
	ConversationID string `json:"-"`
	Text           string `json:"text"`
	Model          string `json:"model"`
}

// ChatEvent is one frame of the chat stream. It is the only shape the client sees, the internal
// runtime events are translated into it before they leave the application layer.
type ChatEvent struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Delta string `json:"delta,omitempty"`
	Error string `json:"error,omitempty"`
}
