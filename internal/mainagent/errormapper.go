package mainagent

import (
	"errors"

	"github.com/jrswab/axe/pkg/runner"
)

// ErrorMapper translates errors into user-facing messages.
type ErrorMapper interface {
	Map(err error) string
}

// AxeErrorMapper maps Axe-specific error types to clean Telegram messages.
type AxeErrorMapper struct{}

const (
	fallbackMsg         = "I couldn't process that request. Please try again."
	configMsg           = "Configuration issue: please check your agent files, API keys, and model settings."
	budgetMsg           = "⚠️ Token budget exceeded. Try simplifying your request or increase the budget in your config."
	authMsg             = "Authentication failed. Please check your API key configuration."
	rateLimitMsg        = "Rate limited. Please wait a moment and try again."
	timeoutMsg          = "Request timed out. Please try again."
	serverMsg           = "The AI service is experiencing issues. Please try again later."
	badRequestMsg       = "Invalid request. Please check your configuration."
	runtimeFallbackMsg  = "Something went wrong. Please try again."
)

// Map implements ErrorMapper.
func (a *AxeErrorMapper) Map(err error) string {
	if err == nil {
		return fallbackMsg
	}

	var ce *runner.ConfigError
	if errors.As(err, &ce) {
		return configMsg
	}

	var be *runner.BudgetExceededError
	if errors.As(err, &be) {
		return budgetMsg
	}

	var re *runner.RuntimeError
	if errors.As(err, &re) {
		if cat, ok := runner.ProviderCategory(err); ok {
			switch cat {
			case "auth":
				return authMsg
			case "rate_limit":
				return rateLimitMsg
			case "timeout":
				return timeoutMsg
			case "server", "overloaded":
				return serverMsg
			case "bad_request":
				return badRequestMsg
			default:
				return runtimeFallbackMsg
			}
		}
		return runtimeFallbackMsg
	}

	return fallbackMsg
}
