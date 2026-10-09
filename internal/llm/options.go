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

type deterministicKey struct{}

// DeterministicSeed is the fixed seed sent with deterministic requests.
const DeterministicSeed = 42

// WithDeterministic asks for repeatable output: temperature 0 and a fixed
// seed. Without it a temperature of 0 means "the model's default", which
// for Ollama models is usually 0.6 to 0.8 and gives different summaries
// and tags for the same transcript.
func WithDeterministic(ctx context.Context) context.Context {
	return context.WithValue(ctx, deterministicKey{}, true)
}

// DeterministicFrom reports whether WithDeterministic was set on ctx.
func DeterministicFrom(ctx context.Context) bool {
	v, _ := ctx.Value(deterministicKey{}).(bool)
	return v
}
