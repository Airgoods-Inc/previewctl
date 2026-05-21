package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestShouldRecordCLIActivity(t *testing.T) {
	root := &cobra.Command{Use: "previewctl"}
	status := &cobra.Command{Use: "status"}
	list := &cobra.Command{Use: "list"}
	service := &cobra.Command{Use: "service"}
	serviceLogs := &cobra.Command{Use: "logs"}
	root.AddCommand(status, list, service)
	service.AddCommand(serviceLogs)

	tests := []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		{name: "status", cmd: status, want: true},
		{name: "nested service logs", cmd: serviceLogs, want: true},
		{name: "list", cmd: list, want: false},
		{name: "root", cmd: root, want: false},
		{name: "nil", cmd: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRecordCLIActivity(tt.cmd); got != tt.want {
				t.Fatalf("shouldRecordCLIActivity() = %v, want %v", got, tt.want)
			}
		})
	}
}
