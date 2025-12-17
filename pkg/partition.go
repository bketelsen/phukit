package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PartitionScheme defines the disk partitioning layout
type PartitionScheme struct {
	EFIPartition   string // EFI System Partition
	BootPartition  string // /boot partition
	Root1Partition string // First root filesystem partition (20GB)
	Root2Partition string // Second root filesystem partition (20GB)
	VarPartition   string // /var partition (remaining space)
}

// CreatePartitions creates a GPT partition table with EFI, boot, and root partitions
func CreatePartitions(device string, dryRun bool) (*PartitionScheme, error) {
	if dryRun {
		fmt.Printf("[DRY RUN] Would create partitions on %s\n", device)
		deviceBase := filepath.Base(device)
		return &PartitionScheme{
			EFIPartition:   "/dev/" + deviceBase + "1",
			BootPartition:  "/dev/" + deviceBase + "2",
			Root1Partition: "/dev/" + deviceBase + "3",
			Root2Partition: "/dev/" + deviceBase + "4",
			VarPartition:   "/dev/" + deviceBase + "5",
		}, nil
	}

	fmt.Println("Creating GPT partition table...")

	// Use sgdisk to create partitions
	// Partition 1: EFI System Partition (2GB)
	// Partition 2: /boot partition (1GB)
	// Partition 3: First root filesystem (20GB)
	// Partition 4: Second root filesystem (20GB)
	// Partition 5: /var partition (remaining space)

	commands := [][]string{
		// Create GPT partition table
		{"sgdisk", "--clear", device},
		// Create EFI partition (2GB, type EF00)
		{"sgdisk", "--new=1:0:+2G", "--typecode=1:EF00", "--change-name=1:EFI", device},
		// Create boot partition (1GB, type 8300)
		{"sgdisk", "--new=2:0:+1G", "--typecode=2:8300", "--change-name=2:boot", device},
		// Create first root partition (20GB, type 8300)
		{"sgdisk", "--new=3:0:+20G", "--typecode=3:8300", "--change-name=3:root1", device},
		// Create second root partition (20GB, type 8300)
		{"sgdisk", "--new=4:0:+20G", "--typecode=4:8300", "--change-name=4:root2", device},
		// Create /var partition (remaining space, type 8300)
		{"sgdisk", "--new=5:0:0", "--typecode=5:8300", "--change-name=5:var", device},
	}

	for _, cmdArgs := range commands {
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to run %s: %w\nOutput: %s", cmdArgs[0], err, string(output))
		}
	}

	// Inform kernel of partition changes
	deviceBase := filepath.Base(device)
	if strings.HasPrefix(deviceBase, "loop") {
		// For loop devices, use losetup --partscan to force partition re-read
		if err := exec.Command("losetup", "--partscan", device).Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: losetup --partscan failed: %v\n", err)
		}
	}
	if err := exec.Command("partprobe", device).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: partprobe failed: %v\n", err)
	}

	// Wait for device nodes to appear
	if err := exec.Command("udevadm", "settle").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: udevadm settle failed: %v\n", err)
	}

	// Determine partition device names
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

	scheme := &PartitionScheme{
		EFIPartition:   part1,
		BootPartition:  part2,
		Root1Partition: part3,
		Root2Partition: part4,
		VarPartition:   part5,
	}

	fmt.Printf("Created partitions:\n")
	fmt.Printf("  EFI:   %s\n", scheme.EFIPartition)
	fmt.Printf("  Boot:  %s\n", scheme.BootPartition)
	fmt.Printf("  Root1: %s\n", scheme.Root1Partition)
	fmt.Printf("  Root2: %s\n", scheme.Root2Partition)
	fmt.Printf("  Var:   %s\n", scheme.VarPartition)

	return scheme, nil
}

// FormatPartitions formats the partitions with appropriate filesystems
func FormatPartitions(scheme *PartitionScheme, dryRun bool) error {
	if dryRun {
		fmt.Println("[DRY RUN] Would format partitions")
		return nil
	}

	fmt.Println("Formatting partitions...")

	// Format EFI partition as FAT32
	fmt.Printf("  Formatting %s as FAT32...\n", scheme.EFIPartition)
	cmd := exec.Command("mkfs.vfat", "-F", "32", "-n", "EFI", scheme.EFIPartition)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to format EFI partition: %w\nOutput: %s", err, string(output))
	}

	// Format boot partition as ext4
	fmt.Printf("  Formatting %s as ext4...\n", scheme.BootPartition)
	cmd = exec.Command("mkfs.ext4", "-F", "-L", "boot", scheme.BootPartition)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to format boot partition: %w\nOutput: %s", err, string(output))
	}

	// Format first root partition as ext4
	fmt.Printf("  Formatting %s as ext4...\n", scheme.Root1Partition)
	cmd = exec.Command("mkfs.ext4", "-F", "-L", "root1", scheme.Root1Partition)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to format root1 partition: %w\nOutput: %s", err, string(output))
	}

	// Format second root partition as ext4
	fmt.Printf("  Formatting %s as ext4...\n", scheme.Root2Partition)
	cmd = exec.Command("mkfs.ext4", "-F", "-L", "root2", scheme.Root2Partition)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to format root2 partition: %w\nOutput: %s", err, string(output))
	}

	// Format /var partition as ext4
	fmt.Printf("  Formatting %s as ext4...\n", scheme.VarPartition)
	cmd = exec.Command("mkfs.ext4", "-F", "-L", "var", scheme.VarPartition)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to format var partition: %w\nOutput: %s", err, string(output))
	}

	fmt.Println("Formatting complete")
	return nil
}

// MountPartitions mounts the partitions to a temporary directory
func MountPartitions(scheme *PartitionScheme, mountPoint string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would mount partitions at %s\n", mountPoint)
		return nil
	}

	fmt.Printf("Mounting partitions at %s...\n", mountPoint)

	// Create mount point if it doesn't exist
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create mount point: %w", err)
	}

	// Mount first root partition
	cmd := exec.Command("mount", scheme.Root1Partition, mountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount root1 partition: %w\nOutput: %s", err, string(output))
	}

	// Create boot and var subdirectories
	bootDir := filepath.Join(mountPoint, "boot")
	varDir := filepath.Join(mountPoint, "var")
	if err := os.MkdirAll(bootDir, 0755); err != nil {
		return fmt.Errorf("failed to create boot directory: %w", err)
	}
	if err := os.MkdirAll(varDir, 0755); err != nil {
		return fmt.Errorf("failed to create var directory: %w", err)
	}

	// Mount boot partition
	cmd = exec.Command("mount", scheme.BootPartition, bootDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount boot partition: %w\nOutput: %s", err, string(output))
	}

	// Create EFI directory after mounting boot (so it's on the boot partition)
	efiDir := filepath.Join(mountPoint, "boot", "efi")
	if err := os.MkdirAll(efiDir, 0755); err != nil {
		return fmt.Errorf("failed to create efi directory: %w", err)
	}

	// Mount EFI partition
	cmd = exec.Command("mount", scheme.EFIPartition, efiDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount EFI partition: %w\nOutput: %s", err, string(output))
	}

	// Mount /var partition
	cmd = exec.Command("mount", scheme.VarPartition, varDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount var partition: %w\nOutput: %s", err, string(output))
	}

	fmt.Println("Partitions mounted successfully")
	return nil
}

// UnmountPartitions unmounts all partitions
func UnmountPartitions(mountPoint string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would unmount partitions at %s\n", mountPoint)
		return nil
	}

	fmt.Println("Unmounting partitions...")

	// Unmount in reverse order
	efiDir := filepath.Join(mountPoint, "boot", "efi")
	bootDir := filepath.Join(mountPoint, "boot")
	varDir := filepath.Join(mountPoint, "var")

	// Unmount EFI
	if err := exec.Command("umount", efiDir).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to unmount EFI: %v\n", err)
	}

	// Unmount boot
	if err := exec.Command("umount", bootDir).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to unmount boot: %v\n", err)
	}

	// Unmount /var
	if err := exec.Command("umount", varDir).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to unmount var: %v\n", err)
	}

	// Unmount root
	if err := exec.Command("umount", mountPoint).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to unmount root: %v\n", err)
	}

	return nil
}

// GetPartitionUUID returns the UUID of a partition
func GetPartitionUUID(partition string) (string, error) {
	cmd := exec.Command("blkid", "-s", "UUID", "-o", "value", partition)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get UUID: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
