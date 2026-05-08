package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Airgoods-Inc/previewctl/src/domain"
	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete an environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			progress := NewCLIProgressReporter()
			mgr, cfg, err := buildManager(progress)
			if err != nil {
				return err
			}

			home, _ := os.UserHomeDir()
			statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")

			envName, err := requireEnv(statePath)
			if err != nil {
				return fmt.Errorf("could not determine environment: %w", err)
			}

			Header(fmt.Sprintf("Deleting environment %s",
				styleDetail.Render(envName)))

			if err := mgr.Destroy(cmd.Context(), envName, domain.DestroyOptions{Force: force}); err != nil {
				return err
			}

			Success(fmt.Sprintf("Environment %s deleted",
				styleDetail.Render(envName)))

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Continue past destroy-hook failures and remove the environment from state regardless")

	return cmd
}
