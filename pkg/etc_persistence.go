package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// PristineEtcPath is where we store the pristine /etc from installation
	PristineEtcPath = "/var/lib/phukit/etc.pristine"
	// VarEtcPath is where user /etc modifications are persisted
	VarEtcPath = "/var/etc"
)

// InstallEtcMountUnit creates a systemd mount unit that bind-mounts /var/etc to /etc
// This ensures /etc changes persist across A/B partition updates
func InstallEtcMountUnit(targetDir string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would install etc.mount systemd unit\n")
		return nil
	}

	fmt.Println("  Installing /etc persistence mount unit...")

	// Create /var/etc directory on target
	varEtcDir := filepath.Join(targetDir, "var", "etc")
	if err := os.MkdirAll(varEtcDir, 0755); err != nil {
		return fmt.Errorf("failed to create /var/etc directory: %w", err)
	}

	// Copy current /etc contents to /var/etc as initial state
	etcSource := filepath.Join(targetDir, "etc")
	cmd := exec.Command("rsync", "-a", etcSource+"/", varEtcDir+"/")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy /etc to /var/etc: %w\nOutput: %s", err, string(output))
	}

	// Create the systemd mount unit
	// Note: etc.mount is a special name that systemd auto-generates for /etc
	// We need to use a different approach - use a .mount unit with escaped path
	mountUnitContent := `[Unit]
Description=Bind mount /var/etc to /etc for persistence
DefaultDependencies=no
After=var.mount local-fs-pre.target
Requires=var.mount
Before=local-fs.target sysinit.target
ConditionPathExists=/var/etc

[Mount]
What=/var/etc
Where=/etc
Type=none
Options=bind

[Install]
WantedBy=local-fs.target
`

	// Write mount unit to /usr/lib/systemd/system/
	systemdDir := filepath.Join(targetDir, "usr", "lib", "systemd", "system")
	if err := os.MkdirAll(systemdDir, 0755); err != nil {
		return fmt.Errorf("failed to create systemd directory: %w", err)
	}

	mountUnitPath := filepath.Join(systemdDir, "etc.mount")
	if err := os.WriteFile(mountUnitPath, []byte(mountUnitContent), 0644); err != nil {
		return fmt.Errorf("failed to write etc.mount unit: %w", err)
	}

	// Enable the mount unit by creating symlink in local-fs.target.wants
	wantsDir := filepath.Join(systemdDir, "local-fs.target.wants")
	if err := os.MkdirAll(wantsDir, 0755); err != nil {
		return fmt.Errorf("failed to create local-fs.target.wants directory: %w", err)
	}

	symlinkPath := filepath.Join(wantsDir, "etc.mount")
	// Remove existing symlink if present
	_ = os.Remove(symlinkPath)
	// Use relative symlink so it works after boot
	if err := os.Symlink("../etc.mount", symlinkPath); err != nil {
		return fmt.Errorf("failed to enable etc.mount unit: %w", err)
	}

	// Also enable in sysinit.target.wants for earlier activation
	sysinitWantsDir := filepath.Join(systemdDir, "sysinit.target.wants")
	if err := os.MkdirAll(sysinitWantsDir, 0755); err != nil {
		return fmt.Errorf("failed to create sysinit.target.wants directory: %w", err)
	}

	sysinitSymlinkPath := filepath.Join(sysinitWantsDir, "etc.mount")
	_ = os.Remove(sysinitSymlinkPath)
	if err := os.Symlink("../etc.mount", sysinitSymlinkPath); err != nil {
		return fmt.Errorf("failed to enable etc.mount in sysinit.target: %w", err)
	}

	fmt.Println("  /etc persistence mount unit installed")
	return nil
}

// SavePristineEtc saves a copy of the pristine /etc after installation
// This is used to detect user modifications during updates
func SavePristineEtc(targetDir string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would save pristine /etc to %s\n", PristineEtcPath)
		return nil
	}

	fmt.Println("  Saving pristine /etc for future updates...")

	etcSource := filepath.Join(targetDir, "etc")
	pristineDest := filepath.Join(targetDir, "var", "lib", "phukit", "etc.pristine")

	// Create directory
	if err := os.MkdirAll(filepath.Dir(pristineDest), 0755); err != nil {
		return fmt.Errorf("failed to create pristine etc directory: %w", err)
	}

	// Use rsync to copy /etc
	cmd := exec.Command("rsync", "-a", "--delete", etcSource+"/", pristineDest+"/")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to save pristine /etc: %w\nOutput: %s", err, string(output))
	}

	fmt.Printf("  Saved pristine /etc snapshot\n")
	return nil
}

// MergeEtcFromActive merges /etc configuration during updates
// With the /var/etc bind mount approach:
// - /var/etc is on the shared /var partition (persists across A/B updates)
// - New container's /etc may have new files that need to be added to /var/etc
// - We merge new files from container /etc into /var/etc, preserving user modifications
//
// Parameters:
// - targetDir: mount point of the NEW root partition (e.g., /tmp/phukit-update)
// - activeRootPartition: the CURRENT root partition device (not used with /var/etc approach)
// - dryRun: if true, don't make changes
func MergeEtcFromActive(targetDir string, activeRootPartition string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would merge /etc from active system\n")
		return nil
	}

	fmt.Println("  Merging /etc configuration...")

	// With the /var/etc bind mount approach, /var/etc is on the shared /var partition.
	// During update, we need to:
	// 1. Detect the partition scheme to find /var
	// 2. Mount /var if not already mounted
	// 3. Merge new files from container's /etc into /var/etc
	// 4. Install the etc.mount unit on the new root

	// Derive device from active root partition
	device := activeRootPartition
	// Strip partition number suffix (e.g., /dev/sda2 -> /dev/sda, /dev/nvme0n1p2 -> /dev/nvme0n1)
	for _, suffix := range []string{"p2", "p3", "2", "3"} {
		device = strings.TrimSuffix(device, suffix)
	}

	scheme, err := DetectExistingPartitionScheme(device)
	if err != nil {
		return fmt.Errorf("failed to detect partition scheme: %w", err)
	}

	// Mount /var partition temporarily
	varMountPoint := "/tmp/phukit-var-merge"
	if err := os.MkdirAll(varMountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create var mount point: %w", err)
	}
	defer func() { _ = os.RemoveAll(varMountPoint) }()

	varCmd := exec.Command("mount", scheme.VarPartition, varMountPoint)
	if err := varCmd.Run(); err != nil {
		return fmt.Errorf("failed to mount /var partition: %w", err)
	}
	defer func() { _ = exec.Command("umount", varMountPoint).Run() }()

	// /var/etc location on the mounted /var partition
	varEtc := filepath.Join(varMountPoint, "etc")

	// Check if /var/etc exists (it should if system was installed with phukit)
	if _, err := os.Stat(varEtc); os.IsNotExist(err) {
		fmt.Println("  No existing /var/etc found, creating from container...")
		if err := os.MkdirAll(varEtc, 0755); err != nil {
			return fmt.Errorf("failed to create /var/etc: %w", err)
		}
		// Copy container's /etc to /var/etc
		containerEtc := filepath.Join(targetDir, "etc")
		cmd := exec.Command("rsync", "-a", containerEtc+"/", varEtc+"/")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to copy /etc to /var/etc: %w\nOutput: %s", err, string(output))
		}
		fmt.Println("  Created /var/etc from container defaults")
	} else {
		// /var/etc exists - merge new files from container without overwriting user modifications
		containerEtc := filepath.Join(targetDir, "etc")
		fmt.Println("  Merging new configuration files from container...")

		// Files that should always come from the container (system identity files)
		systemFiles := map[string]bool{
			"os-release": true,
		}

		err = filepath.Walk(containerEtc, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return nil // Skip files we can't access
			}

			relPath, _ := filepath.Rel(containerEtc, path)
			if relPath == "." {
				return nil
			}

			destPath := filepath.Join(varEtc, relPath)

			// Always copy system identity files from container
			if systemFiles[filepath.Base(relPath)] {
				if info.IsDir() {
					return nil
				}
				if err := copyFile(path, destPath); err != nil {
					fmt.Printf("    Warning: failed to copy %s: %v\n", relPath, err)
				}
				return nil
			}

			// Only copy if destination doesn't exist (preserve user modifications)
			if _, err := os.Stat(destPath); os.IsNotExist(err) {
				if info.IsDir() {
					_ = os.MkdirAll(destPath, info.Mode())
				} else {
					_ = os.MkdirAll(filepath.Dir(destPath), 0755)
					if err := copyFile(path, destPath); err != nil {
						fmt.Printf("    Warning: failed to copy new file %s: %v\n", relPath, err)
					}
				}
			}
			return nil
		})

		if err != nil {
			return fmt.Errorf("failed to merge /etc: %w", err)
		}
	}

	// Install the etc.mount unit on the new root partition
	// This ensures /var/etc is bind-mounted to /etc on boot
	if err := InstallEtcMountUnit(targetDir, dryRun); err != nil {
		return fmt.Errorf("failed to install etc.mount unit: %w", err)
	}

	fmt.Println("  /etc configuration merged successfully")
	return nil
}

// copyFile copies a single file preserving permissions
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	if _, err := srcFile.WriteTo(dstFile); err != nil {
		return err
	}

	return nil
}
