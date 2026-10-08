package llm

import "context"

type thinkingKey struct{}

// WithThinking asks the provider to turn a reasoning model's thinking on or
// off for requests made with ctx. Providers apply it only to models that
// support thinking; others are unaffected.
func WithThinking(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, thinkingKey{}, enabled)
}

// ThinkingFrom returns the thinking preference on ctx, if one was set.
func ThinkingFrom(ctx context.Context) (enabled bool, set bool) {
	v, ok := ctx.Value(thinkingKey{}).(bool)
	return v, ok
}
