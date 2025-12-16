package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ContainerExtractor handles extracting container images to disk
type ContainerExtractor struct {
	ImageRef  string
	TargetDir string
	Verbose   bool
}

// NewContainerExtractor creates a new ContainerExtractor
func NewContainerExtractor(imageRef, targetDir string) *ContainerExtractor {
	return &ContainerExtractor{
		ImageRef:  imageRef,
		TargetDir: targetDir,
	}
}

// SetVerbose enables verbose output
func (c *ContainerExtractor) SetVerbose(verbose bool) {
	c.Verbose = verbose
}

// Extract extracts the container filesystem to the target directory
func (c *ContainerExtractor) Extract() error {
	fmt.Printf("Extracting container image %s...\n", c.ImageRef)

	// Create a temporary container
	containerName := "phukit-extract-" + strings.Replace(strings.Replace(c.ImageRef, "/", "-", -1), ":", "-", -1)

	// Remove any existing container with the same name
	exec.Command("podman", "rm", "-f", containerName).Run()

	// Create container from image
	fmt.Println("  Creating temporary container...")
	cmd := exec.Command("podman", "create", "--name", containerName, c.ImageRef, "/bin/sh")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create container: %w\nOutput: %s", err, string(output))
	}

	// Ensure cleanup
	defer func() {
		if c.Verbose {
			fmt.Println("  Cleaning up temporary container...")
		}
		exec.Command("podman", "rm", "-f", containerName).Run()
	}()

	// Export container filesystem
	fmt.Println("  Exporting container filesystem...")
	cmd = exec.Command("podman", "export", containerName)
	exportOutput, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create export pipe: %w", err)
	}

	// Extract to target directory using tar
	tarCmd := exec.Command("tar", "-xf", "-", "-C", c.TargetDir)
	tarCmd.Stdin = exportOutput
	if c.Verbose {
		tarCmd.Stdout = os.Stdout
		tarCmd.Stderr = os.Stderr
	}

	// Start both commands
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start export: %w", err)
	}

	if err := tarCmd.Start(); err != nil {
		return fmt.Errorf("failed to start tar: %w", err)
	}

	// Wait for both to complete
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	if err := tarCmd.Wait(); err != nil {
		return fmt.Errorf("tar extraction failed: %w", err)
	}

	fmt.Println("Container filesystem extracted successfully")
	return nil
}

// CreateFstab creates an /etc/fstab file with the proper mount points
func CreateFstab(targetDir string, scheme *PartitionScheme) error {
	fmt.Println("Creating /etc/fstab...")

	// Get UUIDs for partitions
	root1UUID, err := GetPartitionUUID(scheme.Root1Partition)
	if err != nil {
		return fmt.Errorf("failed to get root1 UUID: %w", err)
	}

	root2UUID, err := GetPartitionUUID(scheme.Root2Partition)
	if err != nil {
		return fmt.Errorf("failed to get root2 UUID: %w", err)
	}

	bootUUID, err := GetPartitionUUID(scheme.BootPartition)
	if err != nil {
		return fmt.Errorf("failed to get boot UUID: %w", err)
	}

	efiUUID, err := GetPartitionUUID(scheme.EFIPartition)
	if err != nil {
		return fmt.Errorf("failed to get EFI UUID: %w", err)
	}

	varUUID, err := GetPartitionUUID(scheme.VarPartition)
	if err != nil {
		return fmt.Errorf("failed to get var UUID: %w", err)
	}

	// Create fstab content
	fstabContent := fmt.Sprintf(`# /etc/fstab
# Created by phukit

# Root filesystem (root1 - active)
UUID=%s	/		ext4	defaults	0 1

# Second root filesystem (root2 - inactive/alternate)
# UUID=%s	/		ext4	defaults	0 1

# Boot partition
UUID=%s	/boot		ext4	defaults	0 2

# EFI System Partition
UUID=%s	/boot/efi	vfat	umask=0077	0 2

# Var partition
UUID=%s	/var		ext4	defaults	0 2
`, root1UUID, root2UUID, bootUUID, efiUUID, varUUID)

	// Write fstab
	fstabPath := filepath.Join(targetDir, "etc", "fstab")
	if err := os.WriteFile(fstabPath, []byte(fstabContent), 0644); err != nil {
		return fmt.Errorf("failed to write fstab: %w", err)
	}

	fmt.Println("  Created /etc/fstab")
	return nil
}

// SetupSystemDirectories creates necessary system directories
func SetupSystemDirectories(targetDir string) error {
	fmt.Println("Setting up system directories...")

	directories := []string{
		"dev",
		"proc",
		"sys",
		"run",
		"tmp",
		"var/tmp",
		"boot/efi",
	}

	for _, dir := range directories {
		path := filepath.Join(targetDir, dir)
		if err := os.MkdirAll(path, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Set proper permissions for tmp directories
	os.Chmod(filepath.Join(targetDir, "tmp"), 01777)
	os.Chmod(filepath.Join(targetDir, "var", "tmp"), 01777)

	fmt.Println("System directories created")
	return nil
}

// ChrootCommand runs a command in a chroot environment
func ChrootCommand(targetDir string, command string, args ...string) error {
	// Mount necessary filesystems for chroot
	mounts := [][]string{
		{"mount", "--bind", "/dev", filepath.Join(targetDir, "dev")},
		{"mount", "--bind", "/proc", filepath.Join(targetDir, "proc")},
		{"mount", "--bind", "/sys", filepath.Join(targetDir, "sys")},
		{"mount", "--bind", "/run", filepath.Join(targetDir, "run")},
	}

	for _, mount := range mounts {
		if err := exec.Command(mount[0], mount[1:]...).Run(); err != nil {
			// Continue even if mount fails (might already be mounted)
			continue
		}
	}

	// Cleanup function to unmount
	defer func() {
		exec.Command("umount", filepath.Join(targetDir, "run")).Run()
		exec.Command("umount", filepath.Join(targetDir, "sys")).Run()
		exec.Command("umount", filepath.Join(targetDir, "proc")).Run()
		exec.Command("umount", filepath.Join(targetDir, "dev")).Run()
	}()

	// Build chroot command
	chrootArgs := []string{targetDir, command}
	chrootArgs = append(chrootArgs, args...)

	cmd := exec.Command("chroot", chrootArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}
