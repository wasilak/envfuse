package exec

import (
	"fmt"
	"os"
	osexec "os/exec"
	"sort"

	"envfuse/internal/state"
)

func LaunchWithEnv(command []string, inherited []string, envPayload map[string]string) error {
	if len(command) == 0 {
		return nil
	}

	cmd := osexec.Command(command[0], command[1:]...)
	cmd.Env = mergeEnv(inherited, envPayload)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launch child: %w", err)
	}

	return nil
}

func RestartWithCommittedEnv(command []string, inherited []string, store *state.Store) error {
	if store == nil {
		return fmt.Errorf("restart handoff requires store")
	}
	return LaunchWithEnv(command, inherited, store.LastAppliedEnv())
}

func mergeEnv(inherited []string, envPayload map[string]string) []string {
	merged := make(map[string]string, len(inherited)+len(envPayload))
	for _, item := range inherited {
		eq := -1
		for i := 0; i < len(item); i++ {
			if item[i] == '=' {
				eq = i
				break
			}
		}
		if eq <= 0 {
			continue
		}
		merged[item[:eq]] = item[eq+1:]
	}

	for k, v := range envPayload {
		merged[k] = v
	}

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s=%s", k, merged[k]))
	}
	return out
}
