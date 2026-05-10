package mainagent

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jrswab/axe/pkg/runner"
)

func TestMapNilError(t *testing.T) {
	mapper := &AxeErrorMapper{}
	got := mapper.Map(nil)
	want := "I couldn't process that request. Please try again."
	if got != want {
		t.Errorf("Map(nil) = %q, want %q", got, want)
	}
}

func TestMapConfigError(t *testing.T) {
	mapper := &AxeErrorMapper{}
	got := mapper.Map(&runner.ConfigError{Msg: "missing agent"})
	want := "Configuration issue: please check your agent files, API keys, and model settings."
	if got != want {
		t.Errorf("Map(ConfigError) = %q, want %q", got, want)
	}
}

func TestMapPlainError(t *testing.T) {
	mapper := &AxeErrorMapper{}
	got := mapper.Map(errors.New("plain error"))
	want := "I couldn't process that request. Please try again."
	if got != want {
		t.Errorf("Map(plain error) = %q, want %q", got, want)
	}
	// Must not leak raw error text
	if got != want {
		return
	}
	if got == "plain error" || got == "I couldn't process that request. Please try again." && want == "I couldn't process that request. Please try again." {
		// ok
	}
}

func TestMapBudgetExceededError(t *testing.T) {
	mapper := &AxeErrorMapper{}

	// Non-zero values
	got := mapper.Map(&runner.BudgetExceededError{Used: 150, Max: 100})
	want := "⚠️ Token budget exceeded. Try simplifying your request or increase the budget in your config."
	if got != want {
		t.Errorf("Map(BudgetExceededError{150,100}) = %q, want %q", got, want)
	}

	// Zero values (message unchanged)
	got = mapper.Map(&runner.BudgetExceededError{Used: 0, Max: 0})
	if got != want {
		t.Errorf("Map(BudgetExceededError{0,0}) = %q, want %q", got, want)
	}
}

func TestMapRuntimeErrorWithoutProviderError(t *testing.T) {
	mapper := &AxeErrorMapper{}
	re := &runner.RuntimeError{Msg: "runtime failed", Err: errors.New("some error")}
	got := mapper.Map(re)
	want := "Something went wrong. Please try again."
	if got != want {
		t.Errorf("Map(RuntimeError no provider) = %q, want %q", got, want)
	}
}

func TestMapWrappedConfigError(t *testing.T) {
	mapper := &AxeErrorMapper{}
	ce := &runner.ConfigError{Msg: "wrapped"}
	wrapped := fmt.Errorf("outer: %w", ce)
	got := mapper.Map(wrapped)
	want := "Configuration issue: please check your agent files, API keys, and model settings."
	if got != want {
		t.Errorf("Map(wrapped ConfigError) = %q, want %q", got, want)
	}
}
