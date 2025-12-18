package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// BootcInstaller handles bootc container installation
type BootcInstaller struct {
	ImageRef   string
	Device     string
	Verbose    bool
	DryRun     bool
	KernelArgs []string
	MountPoint string
	Output     *OutputWriter
}

// NewBootcInstaller creates a new BootcInstaller
func NewBootcInstaller(imageRef, device string) *BootcInstaller {
	return &BootcInstaller{
		ImageRef:   imageRef,
		Device:     device,
		KernelArgs: []string{},
		MountPoint: "/tmp/phukit-install",
		Output:     NewOutputWriter(OutputFormatText, os.Stdout, false),
	}
}

// SetOutput sets the output writer
func (b *BootcInstaller) SetOutput(output *OutputWriter) {
	b.Output = output
}

// SetVerbose enables verbose output
func (b *BootcInstaller) SetVerbose(verbose bool) {
	b.Verbose = verbose
}

// SetDryRun enables dry run mode
func (b *BootcInstaller) SetDryRun(dryRun bool) {
	b.DryRun = dryRun
}

// AddKernelArg adds a kernel argument
func (b *BootcInstaller) AddKernelArg(arg string) {
	b.KernelArgs = append(b.KernelArgs, arg)
}

// SetMountPoint sets the temporary mount point for installation
func (b *BootcInstaller) SetMountPoint(mountPoint string) {
	b.MountPoint = mountPoint
}

// CheckPodmanAvailable is deprecated - container operations now use go-containerregistry
// Kept for backwards compatibility but does nothing
func CheckPodmanAvailable() error {
	return nil
}

// CheckRequiredTools checks if required tools are available
func CheckRequiredTools() error {
	tools := []string{
		"sgdisk",
		"mkfs.vfat",
		"mkfs.ext4",
		"mount",
		"umount",
		"blkid",
		"partprobe",
	}

	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s not found: %w", tool, err)
		}
	}

	return nil
}

// PullImage validates the image reference and checks if it's accessible
// The actual image pull happens during Extract() to avoid duplicate work
func (b *BootcInstaller) PullImage() error {
	if b.DryRun {
		b.Output.Log(fmt.Sprintf("[DRY RUN] Would pull image: %s", b.ImageRef))
		return nil
	}

	b.Output.Log(fmt.Sprintf("Validating image reference: %s", b.ImageRef))

	// Parse and validate the image reference
	ref, err := name.ParseReference(b.ImageRef)
	if err != nil {
		return fmt.Errorf("invalid image reference: %w", err)
	}

	if b.Verbose && !b.Output.IsJSON() {
		b.Output.Log(fmt.Sprintf("  Image: %s", ref.String()))
	}

	// Try to get image descriptor to verify it exists and is accessible
	// This is a lightweight check that doesn't download layers
	_, err = remote.Head(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("failed to access image: %w (check credentials if private registry)", err)
	}

	b.Output.Log("  Image reference is valid and accessible")
	return nil
}

// Install performs the bootc installation to the target disk
func (b *BootcInstaller) Install() error {
	if b.DryRun {
		b.Output.Log(fmt.Sprintf("[DRY RUN] Would install %s to %s", b.ImageRef, b.Device))
		if len(b.KernelArgs) > 0 {
			b.Output.Log(fmt.Sprintf("[DRY RUN] With kernel arguments: %s", strings.Join(b.KernelArgs, " ")))
		}
		return nil
	}

	b.Output.SetPhase("install", 6)

	if !b.Output.IsJSON() {
		b.Output.Log("Installing bootc image to disk...")
		b.Output.Log(fmt.Sprintf("  Image:  %s", b.ImageRef))
		b.Output.Log(fmt.Sprintf("  Device: %s", b.Device))
	}

	// Step 1: Create partitions
	b.Output.PhaseStart(1, "Create Partitions")
	scheme, err := CreatePartitions(b.Device, b.DryRun)
	if err != nil {
		b.Output.Error(fmt.Errorf("failed to create partitions: %w", err))
		return fmt.Errorf("failed to create partitions: %w", err)
	}
	b.Output.PhaseComplete(1, "Create Partitions")

	// Step 2: Format partitions
	b.Output.PhaseStart(2, "Format Partitions")
	if err := FormatPartitions(scheme, b.DryRun); err != nil {
		b.Output.Error(fmt.Errorf("failed to format partitions: %w", err))
		return fmt.Errorf("failed to format partitions: %w", err)
	}
	b.Output.PhaseComplete(2, "Format Partitions")

	// Step 3: Mount partitions
	b.Output.PhaseStart(3, "Mount Partitions")
	if err := MountPartitions(scheme, b.MountPoint, b.DryRun); err != nil {
		b.Output.Error(fmt.Errorf("failed to mount partitions: %w", err))
		return fmt.Errorf("failed to mount partitions: %w", err)
	}
	b.Output.PhaseComplete(3, "Mount Partitions")

	// Ensure cleanup on error
	defer func() {
		if !b.DryRun {
			if !b.Output.IsJSON() {
				b.Output.Log("\nCleaning up...")
			}
			_ = UnmountPartitions(b.MountPoint, b.DryRun)
			_ = os.RemoveAll(b.MountPoint)
		}
	}()

	// Step 4: Extract container filesystem
	b.Output.PhaseStart(4, "Extract Container Filesystem")
	extractor := NewContainerExtractor(b.ImageRef, b.MountPoint)
	extractor.SetVerbose(b.Verbose)
	if err := extractor.Extract(); err != nil {
		b.Output.Error(fmt.Errorf("failed to extract container: %w", err))
		return fmt.Errorf("failed to extract container: %w", err)
	}
	b.Output.PhaseComplete(4, "Extract Container Filesystem")

	// Step 5: Configure system
	b.Output.PhaseStart(5, "Configure System")

	// Create fstab
	if err := CreateFstab(b.MountPoint, scheme); err != nil {
		b.Output.Error(fmt.Errorf("failed to create fstab: %w", err))
		return fmt.Errorf("failed to create fstab: %w", err)
	}

	// Setup system directories
	if err := SetupSystemDirectories(b.MountPoint); err != nil {
		b.Output.Error(fmt.Errorf("failed to setup directories: %w", err))
		return fmt.Errorf("failed to setup directories: %w", err)
	}

	// Save pristine /etc for future updates
	if err := SavePristineEtc(b.MountPoint, b.DryRun); err != nil {
		b.Output.Error(fmt.Errorf("failed to save pristine /etc: %w", err))
		return fmt.Errorf("failed to save pristine /etc: %w", err)
	}

	// Write system configuration
	config := &SystemConfig{
		ImageRef:       b.ImageRef,
		Device:         b.Device,
		InstallDate:    time.Now().Format(time.RFC3339),
		KernelArgs:     b.KernelArgs,
		BootloaderType: string(DetectBootloader(b.MountPoint)),
	}
	if err := WriteSystemConfigToTarget(b.MountPoint, config, b.DryRun); err != nil {
		b.Output.Error(fmt.Errorf("failed to write system config: %w", err))
		return fmt.Errorf("failed to write system config: %w", err)
	}
	b.Output.PhaseComplete(5, "Configure System")

	// Step 6: Install bootloader
	b.Output.PhaseStart(6, "Install Bootloader")

	// Parse OS information from the extracted container
	osName := ParseOSRelease(b.MountPoint)
	if b.Verbose && !b.Output.IsJSON() {
		b.Output.Log(fmt.Sprintf("  Detected OS: %s", osName))
	}

	bootloader := NewBootloaderInstaller(b.MountPoint, b.Device, scheme, osName)
	bootloader.SetVerbose(b.Verbose)

	// Add kernel arguments
	for _, arg := range b.KernelArgs {
		bootloader.AddKernelArg(arg)
	}

	// Detect and install appropriate bootloader
	bootloaderType := DetectBootloader(b.MountPoint)
	bootloader.SetType(bootloaderType)

	if err := bootloader.Install(); err != nil {
		b.Output.Error(fmt.Errorf("failed to install bootloader: %w", err))
		return fmt.Errorf("failed to install bootloader: %w", err)
	}
	b.Output.PhaseComplete(6, "Install Bootloader")

	b.Output.Complete(true, nil)
	return nil
}

// Verify performs post-installation verification
func (b *BootcInstaller) Verify() error {
	if b.DryRun {
		b.Output.Log("[DRY RUN] Would verify installation")
		return nil
	}

	b.Output.Log("Verifying installation...")

	// Check if the device has partitions now
	deviceName := strings.TrimPrefix(b.Device, "/dev/")
	diskInfo, err := getDiskInfo(deviceName)
	if err != nil {
		return fmt.Errorf("failed to verify: %w", err)
	}

	if len(diskInfo.Partitions) == 0 {
		return fmt.Errorf("no partitions found on device after installation")
	}

	b.Output.Log(fmt.Sprintf("Found %d partition(s) on %s", len(diskInfo.Partitions), b.Device))
	if !b.Output.IsJSON() {
		for _, part := range diskInfo.Partitions {
			b.Output.Log(fmt.Sprintf("  - %s (%s)", part.Device, FormatSize(part.Size)))
		}
	}

	return nil
}

// InstallComplete performs the complete installation workflow
func (b *BootcInstaller) InstallComplete(skipPull bool) error {
	// Check prerequisites
	b.Output.Log("Checking prerequisites...")
	if err := CheckRequiredTools(); err != nil {
		return fmt.Errorf("missing required tools: %w", err)
	}
	if err := CheckPodmanAvailable(); err != nil {
		return err
	}

	// Validate disk
	b.Output.Log(fmt.Sprintf("Validating disk %s...", b.Device))
	minSize := uint64(10 * 1024 * 1024 * 1024) // 10 GB minimum
	if err := ValidateDisk(b.Device, minSize); err != nil {
		return err
	}

	// Pull image if not skipped
	if !skipPull {
		if err := b.PullImage(); err != nil {
			return err
		}
	}

	// Confirm before wiping (skip prompt in JSON mode)
	if !b.DryRun && !b.Output.IsJSON() {
		b.Output.Log("")
		b.Output.Log(strings.Repeat("=", 60))
		b.Output.Log(fmt.Sprintf("WARNING: This will DESTROY ALL DATA on %s!", b.Device))
		b.Output.Log(strings.Repeat("=", 60))
		fmt.Print("Type 'yes' to continue: ")
		var response string
		_, _ = fmt.Scanln(&response)
		if response != "yes" {
			return fmt.Errorf("installation cancelled by user")
		}
		b.Output.Log("")
	}

	// Wipe disk
	b.Output.Log(fmt.Sprintf("Wiping disk %s...", b.Device))
	if err := WipeDisk(b.Device, b.DryRun); err != nil {
		return err
	}
	if !b.Output.IsJSON() {
		b.Output.Log("")
	}

	// Install
	if err := b.Install(); err != nil {
		b.Output.Complete(false, err)
		return err
	}

	// Verify
	if err := b.Verify(); err != nil {
		b.Output.Warning(fmt.Sprintf("verification failed: %v", err))
	}

	return nil
}
