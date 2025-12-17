package cmd

import (
	"fmt"

	"github.com/frostyard/phukit/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	installImage      string
	installDevice     string
	installSkipPull   bool
	installKernelArgs []string
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install a bootc container to a physical disk",
	Long: `Install a bootc compatible container image to a physical disk.

This command will:
  1. Validate the target disk
  2. Pull the container image (unless --skip-pull is specified)
  3. Wipe the disk (after confirmation)
  4. Create partitions (EFI: 2GB, boot: 1GB, root1: 20GB, root2: 20GB, var: remaining)
  5. Extract container filesystem
  6. Configure system and install bootloader
  7. Verify the installation

The dual root partitions enable A/B updates for system resilience.

Example:
  phukit install --image quay.io/example/myimage:latest --device /dev/sda
  phukit install --image localhost/myimage --device /dev/nvme0n1 --karg console=ttyS0`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)

	installCmd.Flags().StringVarP(&installImage, "image", "i", "", "Container image reference (required)")
	installCmd.Flags().StringVarP(&installDevice, "device", "d", "", "Target disk device (required)")
	installCmd.Flags().BoolVar(&installSkipPull, "skip-pull", false, "Skip pulling the image (use already pulled image)")
	installCmd.Flags().StringArrayVarP(&installKernelArgs, "karg", "k", []string{}, "Kernel argument to pass (can be specified multiple times)")

	_ = installCmd.MarkFlagRequired("image")
	_ = installCmd.MarkFlagRequired("device")
}

func runInstall(cmd *cobra.Command, args []string) error {
	verbose := viper.GetBool("verbose")
	dryRun := viper.GetBool("dry-run")

	// Resolve device path
	device, err := pkg.GetDiskByPath(installDevice)
	if err != nil {
		return fmt.Errorf("invalid device: %w", err)
	}

	if verbose {
		fmt.Printf("Resolved device: %s\n", device)
	}

	// Create installer
	installer := pkg.NewBootcInstaller(installImage, device)
	installer.SetVerbose(verbose)
	installer.SetDryRun(dryRun)

	// Add kernel arguments
	for _, arg := range installKernelArgs {
		installer.AddKernelArg(arg)
	}

	// Run installation
	if err := installer.InstallComplete(installSkipPull); err != nil {
		return err
	}

	if !dryRun {
		fmt.Println()
		fmt.Println("=================================================================")
		fmt.Println("Installation complete! You can now boot from this disk.")
		fmt.Println("Make sure to configure your system's boot order if needed.")
		fmt.Println("=================================================================")
	}

	return nil
}
