package persistent

import "context"

type TurnContext struct {
	UserID string

	// ConversationID identifies a multi-turn conversation. On some platforms, it is also referred to as
	// SessionID, but this can easily be confused with login authentication scenarios.
	ConversationID string
	Turn           int64
}

type TurnContextKey struct{}

func TurnContextFromContext(ctx context.Context) *TurnContext {
	v, ok := ctx.Value(TurnContextKey{}).(*TurnContext)
	if ok {
		if v.UserID == "" || v.ConversationID == "" {
			return nil
		}

		return v
	}

	return nil
}

func WithTurnContext(ctx context.Context, rc *TurnContext) context.Context {
	return context.WithValue(ctx, TurnContextKey{}, rc)
}
