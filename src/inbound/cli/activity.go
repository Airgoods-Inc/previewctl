package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	filestate "github.com/Airgoods-Inc/previewctl/src/outbound/state"
	"github.com/spf13/cobra"
)

func recordCLIActivityBestEffort(cmd *cobra.Command) {
	if globalEnvName == "" {
		return
	}
	if !shouldRecordCLIActivity(cmd) {
		return
	}
	mode, err := resolveMode()
	if err != nil || mode != "remote" {
		return
	}

	cfg, _, err := loadConfigWithMode(mode)
	if err != nil {
		return
	}
	dsn := os.Getenv("PREVIEWCTL_STATE_DSN")
	if dsn == "" {
		return
	}
	adapter, err := filestate.NewPostgresStateAdapter(dsn, cfg.Name)
	if err != nil {
		verboseActivityWarning("connecting to state database: %v", err)
		return
	}
	defer func() { _ = adapter.Close() }()

	if err := adapter.RecordCLIActivity(
		context.Background(),
		globalEnvName,
		time.Now().UTC(),
		commandPath(cmd),
		activityActor(),
		activityMachine(),
	); err != nil {
		verboseActivityWarning("%v", err)
	}
}

func shouldRecordCLIActivity(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	path := commandPath(cmd)
	if path == "" || path == "previewctl" {
		return false
	}

	parts := strings.Fields(path)
	if len(parts) < 2 {
		return false
	}
	switch parts[1] {
	case "list", "migrate", "vet", "clean", "version", "completion", "help":
		return false
	default:
		return true
	}
}

func commandPath(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	return cmd.CommandPath()
}

func activityActor() string {
	for _, key := range []string{"GITHUB_ACTOR", "USER", "USERNAME"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return "unknown"
}

func activityMachine() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "unknown"
	}
	return host
}

func verboseActivityWarning(format string, args ...any) {
	if !globalVerbose {
		return
	}
	fmt.Fprintf(os.Stderr, "warning: could not record previewctl activity: "+format+"\n", args...)
}
