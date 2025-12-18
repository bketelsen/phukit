package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
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

	switch activeBase {
	case root1Base:
		return scheme.Root2Partition, true, nil
	case root2Base:
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
	Output *OutputWriter
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
		Output: NewOutputWriter(OutputFormatText, os.Stdout, false),
	}
}

// SetOutput sets the output writer
func (u *SystemUpdater) SetOutput(output *OutputWriter) {
	u.Output = output
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
	u.Output.Log("Preparing for system update...")

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
		u.Output.Logf("Currently booted from: %s (root1)", scheme.Root1Partition)
		u.Output.Logf("Update target: %s (root2)", u.Target)
	} else {
		u.Output.Logf("Currently booted from: %s (root2)", scheme.Root2Partition)
		u.Output.Logf("Update target: %s (root1)", u.Target)
	}

	return nil
}

// PullImage validates the image reference and checks if it's accessible
// The actual image pull happens during Extract() to avoid duplicate work
func (u *SystemUpdater) PullImage() error {
	if u.Config.DryRun {
		u.Output.Logf("[DRY RUN] Would pull image: %s", u.Config.ImageRef)
		return nil
	}

	u.Output.Logf("Validating image reference: %s", u.Config.ImageRef)

	// Parse and validate the image reference
	ref, err := name.ParseReference(u.Config.ImageRef)
	if err != nil {
		return fmt.Errorf("invalid image reference: %w", err)
	}

	if u.Config.Verbose {
		u.Output.Logf("  Image: %s", ref.String())
	}

	// Try to get image descriptor to verify it exists and is accessible
	_, err = remote.Head(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("failed to access image: %w (check credentials if private registry)", err)
	}

	u.Output.Log("  Image reference is valid and accessible")
	return nil
}

// Update performs the system update
func (u *SystemUpdater) Update() error {
	if u.Config.DryRun {
		u.Output.Logf("[DRY RUN] Would update to partition: %s", u.Target)
		return nil
	}

	u.Output.SetPhase("update", 7)
	u.Output.PhaseStart(1, "Mounting target partition")

	// Step 1: Mount target partition
	if err := os.MkdirAll(u.Config.MountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create mount point: %w", err)
	}

	cmd := exec.Command("mount", u.Target, u.Config.MountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount target partition: %w\nOutput: %s", err, string(output))
	}
	defer func() {
		u.Output.Log("Cleaning up...")
		_ = exec.Command("umount", u.Config.MountPoint).Run()
		_ = os.RemoveAll(u.Config.MountPoint)
	}()

	// Step 2: Clear existing content
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
	u.Output.PhaseStart(3, "Extracting new container filesystem")
	extractor := NewContainerExtractor(u.Config.ImageRef, u.Config.MountPoint)
	extractor.SetVerbose(u.Config.Verbose)
	if err := extractor.Extract(); err != nil {
		return fmt.Errorf("failed to extract container: %w", err)
	}

	// Step 4: Merge /etc configuration from active system
	u.Output.PhaseStart(4, "Preserving user configuration")
	activeRoot := u.Scheme.Root1Partition
	if !u.Active {
		activeRoot = u.Scheme.Root2Partition
	}
	if err := MergeEtcFromActive(u.Config.MountPoint, activeRoot, u.Config.DryRun); err != nil {
		return fmt.Errorf("failed to merge /etc: %w", err)
	}

	// Step 5: Setup system directories
	u.Output.PhaseStart(5, "Setting up system directories")
	if err := SetupSystemDirectories(u.Config.MountPoint); err != nil {
		return fmt.Errorf("failed to setup directories: %w", err)
	}

	// Step 6: Install new kernel and initramfs if present
	u.Output.PhaseStart(6, "Checking for new kernel and initramfs")
	if err := u.InstallKernelAndInitramfs(); err != nil {
		return fmt.Errorf("failed to install kernel/initramfs: %w", err)
	}

	// Step 7: Update bootloader configuration
	u.Output.PhaseStart(7, "Updating bootloader configuration")
	if err := u.UpdateBootloader(); err != nil {
		return fmt.Errorf("failed to update bootloader: %w", err)
	}

	u.Output.Logf("Next boot will use: %s", u.Target)
	u.Output.Log("Reboot to activate the new system")
	u.Output.Complete(true, nil)

	return nil
}

// InstallKernelAndInitramfs checks for new kernel and initramfs in the updated root
// and copies them to /boot partition per UAPI Boot Loader Specification
func (u *SystemUpdater) InstallKernelAndInitramfs() error {
	// Look for kernel modules in the new root's /usr/lib/modules directory
	modulesDir := filepath.Join(u.Config.MountPoint, "usr", "lib", "modules")

	// Find kernel version directories
	entries, err := os.ReadDir(modulesDir)
	if err != nil || len(entries) == 0 {
		u.Output.Log("  No kernel modules found in updated image")
		return nil
	}

	// Per UAPI spec: mount XBOOTLDR at /boot and ESP separately
	bootMountPoint := filepath.Join(os.TempDir(), "phukit-boot-mount")
	if err := os.MkdirAll(bootMountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create boot mount point: %w", err)
	}
	defer func() { _ = os.RemoveAll(bootMountPoint) }()

	cmd := exec.Command("mount", u.Scheme.BootPartition, bootMountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount boot partition: %w\nOutput: %s", err, string(output))
	}
	defer func() { _ = exec.Command("umount", bootMountPoint).Run() }()

	// Mount EFI partition separately (not nested under /boot)
	efiMountPoint := filepath.Join(os.TempDir(), "phukit-efi-mount")
	if err := os.MkdirAll(efiMountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create EFI mount point: %w", err)
	}
	defer func() { _ = os.RemoveAll(efiMountPoint) }()

	cmd = exec.Command("mount", u.Scheme.EFIPartition, efiMountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount EFI partition: %w\nOutput: %s", err, string(output))
	}
	defer func() { _ = exec.Command("umount", efiMountPoint).Run() }()

	// Detect bootloader type
	bootloaderType := u.detectBootloaderTypeFromMount(bootMountPoint, efiMountPoint)
	u.Output.Logf("  Detected bootloader: %s", bootloaderType)

	// Per UAPI spec: kernels always go to /boot (XBOOTLDR) for both bootloaders
	kernelDestDir := bootMountPoint

	// Get existing kernels for comparison
	existingKernels, _ := filepath.Glob(filepath.Join(kernelDestDir, "vmlinuz-*"))
	existingKernelMap := make(map[string]bool)
	for _, k := range existingKernels {
		existingKernelMap[filepath.Base(k)] = true
	}

	copiedKernel := false
	copiedInitramfs := false

	// Process each kernel version directory
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		kernelVersion := entry.Name()
		kernelModuleDir := filepath.Join(modulesDir, kernelVersion)

		// Look for kernel in /usr/lib/modules/$KERNEL_VERSION/
		kernelPatterns := []string{
			filepath.Join(kernelModuleDir, "vmlinuz"),
			filepath.Join(kernelModuleDir, "vmlinuz-"+kernelVersion),
		}

		var srcKernel string
		for _, pattern := range kernelPatterns {
			if _, err := os.Stat(pattern); err == nil {
				srcKernel = pattern
				break
			}
		}

		if srcKernel == "" {
			continue // No kernel found for this version
		}

		// Destination kernel name
		kernelName := "vmlinuz-" + kernelVersion
		destKernel := filepath.Join(kernelDestDir, kernelName)

		// Check if kernel needs to be copied
		needsCopy := false
		if !existingKernelMap[kernelName] {
			needsCopy = true
			u.Output.Logf("  Found new kernel: %s", kernelName)
		} else {
			// Compare file sizes to detect changes
			srcInfo, _ := os.Stat(srcKernel)
			dstInfo, _ := os.Stat(destKernel)
			if srcInfo.Size() != dstInfo.Size() {
				needsCopy = true
				u.Output.Logf("  Kernel %s has changed, updating", kernelName)
			}
		}

		if needsCopy {
			if err := copyFile(srcKernel, destKernel); err != nil {
				return fmt.Errorf("failed to copy kernel %s: %w", kernelName, err)
			}
			u.Output.Logf("  Installed kernel: %s", kernelName)
			copiedKernel = true
		}

		// Look for initramfs in /usr/lib/modules/$KERNEL_VERSION/
		initrdPatterns := []string{
			filepath.Join(kernelModuleDir, "initramfs.img"),
			filepath.Join(kernelModuleDir, "initrd.img"),
			filepath.Join(kernelModuleDir, "initramfs-"+kernelVersion+".img"),
			filepath.Join(kernelModuleDir, "initrd.img-"+kernelVersion),
		}

		for _, pattern := range initrdPatterns {
			if srcInitrd, err := os.Stat(pattern); err == nil && !srcInitrd.IsDir() {
				initrdName := "initramfs-" + kernelVersion + ".img"
				destInitrd := filepath.Join(kernelDestDir, initrdName)

				// Check if initramfs needs to be copied
				needsCopy := false
				if dstInitrd, err := os.Stat(destInitrd); os.IsNotExist(err) {
					needsCopy = true
					u.Output.Logf("  Found new initramfs: %s", initrdName)
				} else if err == nil && srcInitrd.Size() != dstInitrd.Size() {
					needsCopy = true
					u.Output.Logf("  Initramfs %s has changed, updating", initrdName)
				}

				if needsCopy {
					if err := copyFile(pattern, destInitrd); err != nil {
						return fmt.Errorf("failed to copy initramfs %s: %w", initrdName, err)
					}
					u.Output.Logf("  Installed initramfs: %s", initrdName)
					copiedInitramfs = true
				}
				break // Only copy the first matching initramfs
			}
		}
	}

	if !copiedKernel && !copiedInitramfs {
		u.Output.Log("  Kernel and initramfs are up to date")
	}

	return nil
}

// detectBootloaderTypeFromMount detects bootloader from already-mounted partitions
func (u *SystemUpdater) detectBootloaderTypeFromMount(bootMount, efiMount string) BootloaderType {
	// Per UAPI spec: systemd-boot entries are in /boot/loader
	loaderDir := filepath.Join(bootMount, "loader")
	if _, err := os.Stat(loaderDir); err == nil {
		return BootloaderSystemdBoot
	}

	// Check for GRUB on boot partition
	grubDirs := []string{
		filepath.Join(bootMount, "grub"),
		filepath.Join(bootMount, "grub2"),
	}
	for _, dir := range grubDirs {
		if _, err := os.Stat(dir); err == nil {
			return BootloaderGRUB2
		}
	}

	// Default to GRUB2
	return BootloaderGRUB2
}

// UpdateBootloader updates the bootloader to boot from the new partition
func (u *SystemUpdater) UpdateBootloader() error {
	// Per UAPI spec: mount XBOOTLDR (/boot) and ESP (/efi) separately
	if err := os.MkdirAll(u.Config.BootMountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create boot mount point: %w", err)
	}

	cmd := exec.Command("mount", u.Scheme.BootPartition, u.Config.BootMountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount boot partition: %w\nOutput: %s", err, string(output))
	}
	defer func() { _ = exec.Command("umount", u.Config.BootMountPoint).Run() }()

	// Mount EFI partition separately (not nested under /boot)
	efiMountPoint := filepath.Join(os.TempDir(), "phukit-update-efi")
	if err := os.MkdirAll(efiMountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create EFI mount point: %w", err)
	}
	defer func() { _ = os.RemoveAll(efiMountPoint) }()

	cmd = exec.Command("mount", u.Scheme.EFIPartition, efiMountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount EFI partition: %w\nOutput: %s", err, string(output))
	}
	defer func() { _ = exec.Command("umount", efiMountPoint).Run() }()

	// Detect bootloader type
	bootloaderType := u.detectBootloaderType()
	u.Output.Logf("  Detected bootloader: %s", bootloaderType)

	// Update based on bootloader type
	switch bootloaderType {
	case BootloaderGRUB2:
		return u.updateGRUBBootloader()
	case BootloaderSystemdBoot:
		return u.updateSystemdBootBootloader()
	default:
		return fmt.Errorf("unsupported bootloader type: %s", bootloaderType)
	}
}

// detectBootloaderType detects which bootloader is installed
func (u *SystemUpdater) detectBootloaderType() BootloaderType {
	// Per UAPI spec: systemd-boot entries are in /boot/loader
	loaderDir := filepath.Join(u.Config.BootMountPoint, "loader")
	if _, err := os.Stat(loaderDir); err == nil {
		return BootloaderSystemdBoot
	}

	// Check for GRUB
	grubDirs := []string{
		filepath.Join(u.Config.BootMountPoint, "grub"),
		filepath.Join(u.Config.BootMountPoint, "grub2"),
	}
	for _, dir := range grubDirs {
		if _, err := os.Stat(dir); err == nil {
			return BootloaderGRUB2
		}
	}

	// Default to GRUB2
	return BootloaderGRUB2
}

// updateGRUBBootloader updates GRUB configuration
func (u *SystemUpdater) updateGRUBBootloader() error {
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

	// Get OS name from the updated system
	osName := ParseOSRelease(u.Config.MountPoint)

	// Find GRUB directory
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

menuentry '%s' {
    linux /vmlinuz-%s %s
    initrd /%s
}

menuentry '%s (Previous)' {
    linux /vmlinuz-%s root=UUID=%s ro console=tty0
    initrd /%s
}
`, osName, kernelVersion, strings.Join(kernelCmdline, " "), initrd,
		osName, kernelVersion, activeUUID, initrd)

	grubCfgPath := filepath.Join(grubDir, "grub.cfg")
	if err := os.WriteFile(grubCfgPath, []byte(grubCfg), 0644); err != nil {
		return fmt.Errorf("failed to write grub.cfg: %w", err)
	}

	u.Output.Logf("  Updated GRUB to boot from %s", u.Target)
	return nil
}

// updateSystemdBootBootloader updates systemd-boot configuration
func (u *SystemUpdater) updateSystemdBootBootloader() error {
	// Get UUIDs
	targetUUID, err := GetPartitionUUID(u.Target)
	if err != nil {
		return fmt.Errorf("failed to get target UUID: %w", err)
	}

	activeRoot := u.Scheme.Root1Partition
	if !u.Active {
		activeRoot = u.Scheme.Root2Partition
	}
	activeUUID, _ := GetPartitionUUID(activeRoot)

	// Per UAPI spec: kernels are on /boot (XBOOTLDR partition)
	kernels, err := filepath.Glob(filepath.Join(u.Config.BootMountPoint, "vmlinuz-*"))
	if err != nil || len(kernels) == 0 {
		return fmt.Errorf("no kernel found in /boot")
	}
	kernel := filepath.Base(kernels[0])
	kernelVersion := strings.TrimPrefix(kernel, "vmlinuz-")

	// Look for initramfs on /boot partition
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
		"rw",
	}
	kernelCmdline = append(kernelCmdline, u.Config.KernelArgs...)

	// Get OS name from the updated system
	osName := ParseOSRelease(u.Config.MountPoint)

	// Per UAPI spec: loader entries are in /boot/loader (XBOOTLDR partition)
	loaderDir := filepath.Join(u.Config.BootMountPoint, "loader")

	// Create/update main boot entry (always points to newest system)
	entriesDir := filepath.Join(loaderDir, "entries")
	if err := os.MkdirAll(entriesDir, 0755); err != nil {
		return fmt.Errorf("failed to create entries directory: %w", err)
	}

	mainEntry := fmt.Sprintf(`title   %s
linux   /vmlinuz-%s
initrd  /%s
options %s
`, osName, kernelVersion, initrd, strings.Join(kernelCmdline, " "))

	mainEntryPath := filepath.Join(entriesDir, "bootc.conf")
	if err := os.WriteFile(mainEntryPath, []byte(mainEntry), 0644); err != nil {
		return fmt.Errorf("failed to write main boot entry: %w", err)
	}

	// Create/update rollback boot entry (points to previous system)
	previousEntry := fmt.Sprintf(`title   %s (Previous)
linux   /vmlinuz-%s
initrd  /%s
options root=UUID=%s ro
`, osName, kernelVersion, initrd, activeUUID)

	previousEntryPath := filepath.Join(entriesDir, "bootc-previous.conf")
	if err := os.WriteFile(previousEntryPath, []byte(previousEntry), 0644); err != nil {
		return fmt.Errorf("failed to write rollback boot entry: %w", err)
	}

	u.Output.Logf("  Updated systemd-boot to boot from %s", u.Target)
	return nil
}

// PerformUpdate performs the complete update workflow
func (u *SystemUpdater) PerformUpdate(skipPull bool) error {
	// Check prerequisites
	u.Output.Log("Checking prerequisites...")
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
		// Interactive prompts always go to stdout for user interaction
		fmt.Printf("\n%s\n", strings.Repeat("=", 60))
		fmt.Printf("This will update the system to a new root filesystem.\n")
		fmt.Printf("Target partition: %s\n", u.Target)
		fmt.Printf("%s\n", strings.Repeat("=", 60))
		fmt.Print("Type 'yes' to continue: ")
		var response string
		_, _ = fmt.Scanln(&response)
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
			u.Output.Warning(fmt.Sprintf("Failed to update system config: %v", err))
		}
	}

	return nil
}
