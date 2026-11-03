package config

import "testing"

func TestMacContextConfiguration(t *testing.T) {
	t.Setenv("MAC_CONTEXT_GENERATION_ENABLED", "true")
	t.Setenv("MAC_CONTEXT_INPUT_TOKEN_BUDGET", "4096")
	c := macContextFromEnv()
	if !c.Enabled || c.InputTokenBudget != 4096 || len(c.validate()) != 0 {
		t.Fatalf("config=%+v", c)
	}
	for _, budget := range []int{0, 255, 8193} {
		if problems := (MacContext{Enabled: true, InputTokenBudget: budget}).validate(); len(problems) == 0 {
			t.Fatalf("invalid budget=%d", budget)
		}
	}
	if len((MacContext{}).validate()) != 0 {
		t.Fatal("disabled zero config broke legacy caller")
	}
	t.Setenv("MAC_CONTEXT_GENERATION_ENABLED", "false")
	if macContextFromEnv().Enabled {
		t.Fatal("rollback switch ignored")
	}
}
