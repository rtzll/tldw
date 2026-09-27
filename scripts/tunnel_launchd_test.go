package scripts

import (
	"os/exec"
	"strings"
	"testing"
)

func TestLaunchdStatusSummary(t *testing.T) {
	for _, tt := range []struct {
		name   string
		report string
		want   []string
		absent []string
	}{
		{
			name: "running after forced stop",
			report: `gui/501/dev.rtzll.tldw.tunnel = {
	state = running
	pid = 29244
	runs = 2
	last terminating signal = Killed: 9
	LWCR = {
		"reqs" => {
			"vers" => 1
		}
	}
	resource coalition = {
		state = active
	}
}
`,
			want: []string{
				"Service:       Running (PID 29244)",
				"Launches:      2 since the service was loaded",
				"Previous stop: Forced stop (SIGKILL). A forced restart can cause this.",
			},
			absent: []string{"Not running", "Next step", "resource coalition", "LWCR"},
		},
		{
			name: "stopped with an error",
			report: `service = {
	state = waiting
	last exit code = 1
}
`,
			want: []string{"Not running (launchd state: waiting)", "Previous exit: Error (code 1)", "check the logs", "just tunnel-launchd-restart"},
		},
		{
			name: "successful exit",
			report: `service = {
	state = not running
	last exit code = 0
}
`,
			want:   []string{"Previous exit: Successful (code 0)"},
			absent: []string{"Previous exit: Error"},
		},
		{
			name: "first launch",
			report: `service = {
	state = running
	pid = 42
	runs = 1
}
`,
			want:   []string{"Running (PID 42)", "Launches:      1"},
			absent: []string{"Previous", "Next step"},
		},
		{
			name: "missing state",
			report: `service = {
}
`,
			want: []string{"launchd state: unknown"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-c", `source ./tunnel-launchd; summarize_agent_status`)
			cmd.Stdin = strings.NewReader(tt.report)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("status formatter failed: %v\n%s", err, output)
			}
			for _, want := range tt.want {
				if !strings.Contains(string(output), want) {
					t.Errorf("missing %q in:\n%s", want, output)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(string(output), absent) {
					t.Errorf("unexpected %q in:\n%s", absent, output)
				}
			}
		})
	}
}
