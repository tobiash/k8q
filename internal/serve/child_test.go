package serve

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestExecChildExitStatus(t *testing.T) {
	for _, code := range []int{0, 7} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Setenv("K8Q_CHILD_EXIT_TEST", strconv.Itoa(code))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err := execChild(ctx, []string{os.Args[0], "-test.run=^TestExecChildExitProcess$"}, "")
			if code == 0 {
				if err != nil {
					t.Errorf("execChild successful leader = %v, want nil after cleanup", err)
				}
				return
			}
			if got, ok := ExitCode(err); !ok || got != code {
				t.Errorf("execChild exit = %d, %v (%v); want %d", got, ok, err, code)
			}
		})
	}
}

func TestExecChildExitProcess(t *testing.T) {
	value := os.Getenv("K8Q_CHILD_EXIT_TEST")
	if value == "" {
		return
	}
	code, err := strconv.Atoi(value)
	if err != nil {
		t.Fatal(err)
	}
	os.Exit(code)
}
