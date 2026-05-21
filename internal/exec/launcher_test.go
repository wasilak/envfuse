package exec

import (
	"testing"

	"envfuse/internal/state"
)

func TestLauncher_EnvMerge(t *testing.T) {
	t.Parallel()

	inherited := []string{"HOME=/tmp/home", "APP_API_KEY=old", "UNCHANGED=yes"}
	payload := map[string]string{"APP_API_KEY": "new", "DB_PASSWORD": "secret"}

	merged := mergeEnv(inherited, payload)

	vals := make(map[string]string, len(merged))
	for _, item := range merged {
		for i := 0; i < len(item); i++ {
			if item[i] == '=' {
				vals[item[:i]] = item[i+1:]
				break
			}
		}
	}

	if vals["APP_API_KEY"] != "new" {
		t.Fatalf("expected APP_API_KEY override to new, got %q", vals["APP_API_KEY"])
	}
	if vals["DB_PASSWORD"] != "secret" {
		t.Fatalf("expected DB_PASSWORD from payload, got %q", vals["DB_PASSWORD"])
	}
	if vals["UNCHANGED"] != "yes" {
		t.Fatalf("expected inherited UNCHANGED to remain yes, got %q", vals["UNCHANGED"])
	}
}

func TestLauncher_RestartHandoffUsesUpdatedEnv(t *testing.T) {
	t.Parallel()

	store := state.NewStore()
	store.StageCandidate(map[string]map[string]any{"app/base": {"API_KEY": "v1"}}, map[string]string{"APP_API_KEY": "v1"}, nil)
	store.CommitCandidate()

	store.StageCandidate(map[string]map[string]any{"app/base": {"API_KEY": "v2"}}, map[string]string{"APP_API_KEY": "v2"}, nil)
	store.CommitCandidate()

	err := RestartWithCommittedEnv(
		[]string{"/bin/sh", "-c", "[ \"$APP_API_KEY\" = \"v2\" ]"},
		[]string{"APP_API_KEY=v1"},
		store,
	)
	if err != nil {
		t.Fatalf("expected restart handoff to launch with updated env, got error: %v", err)
	}
}
