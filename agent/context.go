package agent

import (
	"context"
)

func (a *Agent) restore(conversationID string) error {
	messages, err := a.Store.LoadMessages(context.Background(), conversationID)
	if err != nil {
		return err
	}

	if len(messages) > 0 {
		a.State.Messages = append(a.State.Messages, messages...)
	}

	return nil
}
