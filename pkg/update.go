package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GetActiveRootPartition determines which root partition is currently active
func GetActiveRootPartition() (string, error) {
	// Read /proc/cmdline to see which root is being used
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return "", fmt.Errorf("failed to read /proc/cmdline: %w", err)
	}

	cmdlineStr := string(cmdline)

	// Look for root=UUID=XXX or root=/dev/XXX
	fields := strings.Fields(cmdlineStr)
	for _, field := range fields {
		if strings.HasPrefix(field, "root=UUID=") {
			uuid := strings.TrimPrefix(field, "root=UUID=")
			// Find which partition has this UUID
			return findPartitionByUUID(uuid)
		} else if strings.HasPrefix(field, "root=/dev/") {
			return strings.TrimPrefix(field, "root="), nil
		}
	}

	return "", fmt.Errorf("could not determine active root partition from kernel command line")
}

// findPartitionByUUID finds a partition device path by its UUID
func findPartitionByUUID(uuid string) (string, error) {
	cmd := exec.Command("blkid", "-U", uuid)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to find partition with UUID %s: %w", uuid, err)
	}
	return strings.TrimSpace(string(output)), nil
}

// GetInactiveRootPartition returns the inactive root partition given a partition scheme
func GetInactiveRootPartition(scheme *PartitionScheme) (string, bool, error) {
	active, err := GetActiveRootPartition()
	if err != nil {
		// If we can't determine active, default to root1 as active
		fmt.Fprintf(os.Stderr, "Warning: could not determine active partition: %v\n", err)
		fmt.Fprintf(os.Stderr, "Defaulting to root2 as target\n")
		return scheme.Root2Partition, true, nil
	}

	// Normalize paths for comparison
	activeBase := filepath.Base(active)
	root1Base := filepath.Base(scheme.Root1Partition)
	root2Base := filepath.Base(scheme.Root2Partition)

	if activeBase == root1Base {
		return scheme.Root2Partition, true, nil
	} else if activeBase == root2Base {
		return scheme.Root1Partition, false, nil
	}

	// Active partition doesn't match either root partition
	// This can happen in test scenarios where we're not booted from the target disk
	// Default to root1 as active, root2 as target
	fmt.Fprintf(os.Stderr, "Warning: active partition %s does not match either root partition (%s or %s)\n",
		active, scheme.Root1Partition, scheme.Root2Partition)
	fmt.Fprintf(os.Stderr, "Defaulting to root2 as target\n")
	return scheme.Root2Partition, true, nil
}

// DetectExistingPartitionScheme detects the partition scheme of an existing installation
func DetectExistingPartitionScheme(device string) (*PartitionScheme, error) {
	deviceBase := filepath.Base(device)
	var part1, part2, part3, part4, part5 string

	// Handle different device naming conventions
	// nvme, mmcblk, and loop devices use "p" prefix for partitions
	if strings.HasPrefix(deviceBase, "nvme") || strings.HasPrefix(deviceBase, "mmcblk") || strings.HasPrefix(deviceBase, "loop") {
		part1 = device + "p1"
		part2 = device + "p2"
		part3 = device + "p3"
		part4 = device + "p4"
		part5 = device + "p5"
	} else {
		part1 = device + "1"
		part2 = device + "2"
		part3 = device + "3"
		part4 = device + "4"
		part5 = device + "5"
	}

	// Verify partitions exist
	for _, part := range []string{part1, part2, part3, part4, part5} {
		if _, err := os.Stat(part); os.IsNotExist(err) {
			return nil, fmt.Errorf("partition %s does not exist", part)
		}
	}

	scheme := &PartitionScheme{
		EFIPartition:   part1,
		BootPartition:  part2,
		Root1Partition: part3,
		Root2Partition: part4,
		VarPartition:   part5,
	}

	return scheme, nil
}

// UpdaterConfig holds configuration for system updates
type UpdaterConfig struct {
	Device         string
	ImageRef       string
	Verbose        bool
	DryRun         bool
	Force          bool // Skip interactive confirmation
	KernelArgs     []string
	MountPoint     string
	BootMountPoint string
}

// SystemUpdater handles A/B system updates
type SystemUpdater struct {
	Config UpdaterConfig
	Scheme *PartitionScheme
	Active bool // true if root1 is active, false if root2 is active
	Target string
}

// NewSystemUpdater creates a new SystemUpdater
func NewSystemUpdater(device, imageRef string) *SystemUpdater {
	return &SystemUpdater{
		Config: UpdaterConfig{
			Device:         device,
			ImageRef:       imageRef,
			MountPoint:     "/tmp/phukit-update",
			BootMountPoint: "/tmp/phukit-boot",
		},
	}
}

// SetVerbose enables verbose output
func (u *SystemUpdater) SetVerbose(verbose bool) {
	u.Config.Verbose = verbose
}

// SetDryRun enables dry run mode
func (u *SystemUpdater) SetDryRun(dryRun bool) {
	u.Config.DryRun = dryRun
}

// SetForce enables non-interactive mode (skips confirmation)
func (u *SystemUpdater) SetForce(force bool) {
	u.Config.Force = force
}

// AddKernelArg adds a kernel argument
func (u *SystemUpdater) AddKernelArg(arg string) {
	u.Config.KernelArgs = append(u.Config.KernelArgs, arg)
}

// PrepareUpdate prepares for an update by detecting partitions and determining target
func (u *SystemUpdater) PrepareUpdate() error {
	fmt.Println("Preparing for system update...")

	// Detect existing partition scheme
	scheme, err := DetectExistingPartitionScheme(u.Config.Device)
	if err != nil {
		return fmt.Errorf("failed to detect partition scheme: %w", err)
	}
	u.Scheme = scheme

	// Determine inactive partition
	target, active, err := GetInactiveRootPartition(scheme)
	if err != nil {
		return fmt.Errorf("failed to determine target partition: %w", err)
	}
	u.Target = target
	u.Active = active

	if u.Active {
		fmt.Printf("Currently booted from: %s (root1)\n", scheme.Root1Partition)
		fmt.Printf("Update target: %s (root2)\n", u.Target)
	} else {
		fmt.Printf("Currently booted from: %s (root2)\n", scheme.Root2Partition)
		fmt.Printf("Update target: %s (root1)\n", u.Target)
	}

	return nil
}

// PullImage pulls the container image
func (u *SystemUpdater) PullImage() error {
	if u.Config.DryRun {
		fmt.Printf("[DRY RUN] Would pull image: %s\n", u.Config.ImageRef)
		return nil
	}

	fmt.Printf("Pulling image: %s\n", u.Config.ImageRef)

	args := []string{"pull"}
	if u.Config.Verbose {
		args = append(args, "--log-level=debug")
	}
	args = append(args, u.Config.ImageRef)

	cmd := exec.Command("podman", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}

	return nil
}

// Update performs the system update
func (u *SystemUpdater) Update() error {
	if u.Config.DryRun {
		fmt.Printf("[DRY RUN] Would update to partition: %s\n", u.Target)
		return nil
	}

	fmt.Println("\nStarting system update...")

	// Step 1: Mount target partition
	fmt.Println("\nStep 1/5: Mounting target partition...")
	if err := os.MkdirAll(u.Config.MountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create mount point: %w", err)
	}

	cmd := exec.Command("mount", u.Target, u.Config.MountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount target partition: %w\nOutput: %s", err, string(output))
	}
	defer func() {
		fmt.Println("\nCleaning up...")
		exec.Command("umount", u.Config.MountPoint).Run()
		os.RemoveAll(u.Config.MountPoint)
	}()

	// Step 2: Clear existing content
	fmt.Println("\nStep 2/5: Clearing old content from target partition...")
	entries, err := os.ReadDir(u.Config.MountPoint)
	if err != nil {
		return fmt.Errorf("failed to read target directory: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(u.Config.MountPoint, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}
	}

	// Step 3: Extract new container filesystem
	fmt.Println("\nStep 3/6: Extracting new container filesystem...")
	extractor := NewContainerExtractor(u.Config.ImageRef, u.Config.MountPoint)
	extractor.SetVerbose(u.Config.Verbose)
	if err := extractor.Extract(); err != nil {
		return fmt.Errorf("failed to extract container: %w", err)
	}

	// Step 4: Merge /etc configuration from active system
	fmt.Println("\nStep 4/6: Preserving user configuration...")
	activeRoot := u.Scheme.Root1Partition
	if !u.Active {
		activeRoot = u.Scheme.Root2Partition
	}
	if err := MergeEtcFromActive(u.Config.MountPoint, activeRoot, u.Config.DryRun); err != nil {
		return fmt.Errorf("failed to merge /etc: %w", err)
	}

	// Step 5: Setup system directories
	fmt.Println("\nStep 5/6: Setting up system directories...")
	if err := SetupSystemDirectories(u.Config.MountPoint); err != nil {
		return fmt.Errorf("failed to setup directories: %w", err)
	}

	// Step 6: Update bootloader configuration
	fmt.Println("\nStep 6/6: Updating bootloader configuration...")
	if err := u.UpdateBootloader(); err != nil {
		return fmt.Errorf("failed to update bootloader: %w", err)
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("System update completed successfully!")
	fmt.Printf("Next boot will use: %s\n", u.Target)
	fmt.Println("Reboot to activate the new system")
	fmt.Println(strings.Repeat("=", 60))

	return nil
}

// UpdateBootloader updates the bootloader to boot from the new partition
func (u *SystemUpdater) UpdateBootloader() error {
	// Mount boot partition
	if err := os.MkdirAll(u.Config.BootMountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create boot mount point: %w", err)
	}

	cmd := exec.Command("mount", u.Scheme.BootPartition, u.Config.BootMountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount boot partition: %w\nOutput: %s", err, string(output))
	}
	defer exec.Command("umount", u.Config.BootMountPoint).Run()

	// Get UUID of new root partition
	targetUUID, err := GetPartitionUUID(u.Target)
	if err != nil {
		return fmt.Errorf("failed to get target UUID: %w", err)
	}

	// Find kernel and initramfs
	kernels, err := filepath.Glob(filepath.Join(u.Config.BootMountPoint, "vmlinuz-*"))
	if err != nil || len(kernels) == 0 {
		return fmt.Errorf("no kernel found in /boot")
	}
	kernel := filepath.Base(kernels[0])
	kernelVersion := strings.TrimPrefix(kernel, "vmlinuz-")

	// Look for initramfs
	var initrd string
	initrdPatterns := []string{
		filepath.Join(u.Config.BootMountPoint, "initramfs-"+kernelVersion+".img"),
		filepath.Join(u.Config.BootMountPoint, "initrd.img-"+kernelVersion),
		filepath.Join(u.Config.BootMountPoint, "initramfs-"+kernelVersion),
	}
	for _, pattern := range initrdPatterns {
		if _, err := os.Stat(pattern); err == nil {
			initrd = filepath.Base(pattern)
			break
		}
	}

	// Build kernel command line
	kernelCmdline := []string{
		"root=UUID=" + targetUUID,
		"ro",
		"console=tty0",
	}
	kernelCmdline = append(kernelCmdline, u.Config.KernelArgs...)

	// Update GRUB configuration
	grubDirs := []string{
		filepath.Join(u.Config.BootMountPoint, "grub"),
		filepath.Join(u.Config.BootMountPoint, "grub2"),
	}

	var grubDir string
	for _, dir := range grubDirs {
		if _, err := os.Stat(dir); err == nil {
			grubDir = dir
			break
		}
	}

	if grubDir == "" {
		return fmt.Errorf("could not find grub directory")
	}

	// Create new GRUB config with both boot options
	activeRoot := u.Scheme.Root1Partition
	if !u.Active {
		activeRoot = u.Scheme.Root2Partition
	}

	activeUUID, _ := GetPartitionUUID(activeRoot)

	grubCfg := fmt.Sprintf(`set timeout=5
set default=0

menuentry 'Linux (Updated)' {
    linux /vmlinuz-%s %s
    initrd /%s
}

menuentry 'Linux (Previous)' {
    linux /vmlinuz-%s root=UUID=%s ro console=tty0
    initrd /%s
}
`, kernelVersion, strings.Join(kernelCmdline, " "), initrd,
		kernelVersion, activeUUID, initrd)

	grubCfgPath := filepath.Join(grubDir, "grub.cfg")
	if err := os.WriteFile(grubCfgPath, []byte(grubCfg), 0644); err != nil {
		return fmt.Errorf("failed to write grub.cfg: %w", err)
	}

	fmt.Printf("  Updated bootloader to boot from %s\n", u.Target)
	return nil
}

// PerformUpdate performs the complete update workflow
func (u *SystemUpdater) PerformUpdate(skipPull bool) error {
	// Check prerequisites
	fmt.Println("Checking prerequisites...")
	if err := CheckPodmanAvailable(); err != nil {
		return err
	}

	// Prepare update
	if err := u.PrepareUpdate(); err != nil {
		return err
	}

	// Pull image if not skipped
	if !skipPull {
		if err := u.PullImage(); err != nil {
			return err
		}
	}

	// Confirm update
	if !u.Config.DryRun && !u.Config.Force {
		fmt.Printf("\n" + strings.Repeat("=", 60) + "\n")
		fmt.Printf("This will update the system to a new root filesystem.\n")
		fmt.Printf("Target partition: %s\n", u.Target)
		fmt.Printf(strings.Repeat("=", 60) + "\n")
		fmt.Print("Type 'yes' to continue: ")
		var response string
		fmt.Scanln(&response)
		if response != "yes" {
			return fmt.Errorf("update cancelled by user")
		}
		fmt.Println()
	}

	// Perform update
	if err := u.Update(); err != nil {
		return err
	}

	// Update system config with new image reference
	if !u.Config.DryRun {
		if err := UpdateSystemConfigImageRef(u.Config.ImageRef, u.Config.DryRun); err != nil {
			fmt.Printf("Warning: failed to update system config: %v\n", err)
		}
	}

	return nil
}
