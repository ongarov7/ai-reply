package config

// MacContext bounds optional native desktop context without changing mobile
// requests, plan entitlement, or provider/model selection.
type MacContext struct {
	Enabled          bool
	InputTokenBudget int
}

func macContextFromEnv() MacContext {
	return MacContext{Enabled: boolean("MAC_CONTEXT_GENERATION_ENABLED", true), InputTokenBudget: num("MAC_CONTEXT_INPUT_TOKEN_BUDGET", 4096)}
}

func (m MacContext) validate() []string {
	if !m.Enabled && m.InputTokenBudget == 0 {
		return nil
	}
	if m.InputTokenBudget < 256 || m.InputTokenBudget > 8192 {
		return []string{"MAC_CONTEXT_INPUT_TOKEN_BUDGET must be between 256 and 8192"}
	}
	return nil
}
