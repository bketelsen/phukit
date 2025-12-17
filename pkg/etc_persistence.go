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
)

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

// MergeEtcFromActive performs a 3-way merge of /etc during updates
// This preserves user configuration changes while applying updates
func MergeEtcFromActive(targetDir string, activeRootPartition string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would merge /etc from active system\n")
		return nil
	}

	fmt.Println("  Merging /etc configuration from active system...")

	// Mount the active root partition temporarily
	activeMountPoint := "/tmp/phukit-active-root"
	if err := os.MkdirAll(activeMountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create active mount point: %w", err)
	}
	defer os.RemoveAll(activeMountPoint)

	// Check if already mounted (we're running from this partition)
	// In that case, we can read directly from /
	activeEtcSource := "/etc"
	pristineEtcSource := PristineEtcPath

	// If pristine /etc doesn't exist, we can't do a 3-way merge
	// Fall back to simple copy
	if _, err := os.Stat(pristineEtcSource); os.IsNotExist(err) {
		fmt.Println("  No pristine /etc found, copying all configuration files...")
		return copyActiveEtc(activeEtcSource, filepath.Join(targetDir, "etc"))
	}

	// Perform 3-way merge
	// 1. Find files that differ between pristine and active (user modifications)
	// 2. Copy those modified files to the new /etc
	fmt.Println("  Detecting user-modified configuration files...")
	modifiedFiles, err := findModifiedFiles(pristineEtcSource, activeEtcSource)
	if err != nil {
		return fmt.Errorf("failed to detect modified files: %w", err)
	}

	if len(modifiedFiles) == 0 {
		fmt.Println("  No user modifications detected in /etc")
		return nil
	}

	fmt.Printf("  Found %d modified configuration file(s)\n", len(modifiedFiles))

	// Copy modified files to new /etc
	newEtcDir := filepath.Join(targetDir, "etc")
	for _, relPath := range modifiedFiles {
		srcFile := filepath.Join(activeEtcSource, relPath)
		dstFile := filepath.Join(newEtcDir, relPath)

		// Check if source is a directory
		info, err := os.Stat(srcFile)
		if err != nil {
			fmt.Printf("    Warning: skipping %s (stat error: %v)\n", relPath, err)
			continue
		}

		if info.IsDir() {
			// Create directory in target
			if err := os.MkdirAll(dstFile, info.Mode()); err != nil {
				fmt.Printf("    Warning: failed to create dir %s: %v\n", relPath, err)
			}
			continue
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(dstFile), 0755); err != nil {
			fmt.Printf("    Warning: failed to create parent dir for %s: %v\n", relPath, err)
			continue
		}

		// Copy file
		if err := copyFile(srcFile, dstFile); err != nil {
			fmt.Printf("    Warning: failed to copy %s: %v\n", relPath, err)
			continue
		}

		if len(modifiedFiles) < 20 { // Only show individual files if not too many
			fmt.Printf("    Preserved: %s\n", relPath)
		}
	}

	// Update pristine /etc for next update
	newPristineEtcPath := filepath.Join(targetDir, "var", "lib", "phukit", "etc.pristine")
	newEtcSource := filepath.Join(targetDir, "etc")

	if err := os.MkdirAll(filepath.Dir(newPristineEtcPath), 0755); err != nil {
		return fmt.Errorf("failed to create pristine etc directory: %w", err)
	}

	cmd := exec.Command("rsync", "-a", "--delete", newEtcSource+"/", newPristineEtcPath+"/")
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("    Warning: failed to update pristine /etc: %v\n", err)
		fmt.Printf("    Output: %s\n", string(output))
	}

	fmt.Println("  Configuration merge complete")
	return nil
}

// findModifiedFiles compares pristine and active /etc to find user modifications
func findModifiedFiles(pristineDir, activeDir string) ([]string, error) {
	var modified []string

	// Use rsync's dry-run to detect differences
	cmd := exec.Command("rsync", "-n", "-a", "-i", "--delete", activeDir+"/", pristineDir+"/")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// rsync returns non-zero if there are differences, which is expected
		// Only return error if it's not a diff-related exit code
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() > 1 {
			return nil, fmt.Errorf("rsync failed: %w\nOutput: %s", err, string(output))
		}
	}

	// Parse rsync output to find modified files
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if len(line) < 12 {
			continue
		}
		// rsync itemize format: YXcstpoguax  path/to/file
		// We care about: c (checksum/content change), s (size change), t (time change)
		changeType := line[0:11]
		if strings.Contains(changeType, "c") || strings.Contains(changeType, "s") ||
			(changeType[0] == '>' && changeType[1] == 'f') {
			// Extract filename
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				filename := strings.Join(parts[1:], " ")
				modified = append(modified, filename)
			}
		}
	}

	return modified, nil
}

// copyActiveEtc is a fallback that copies all /etc files
func copyActiveEtc(srcDir, dstDir string) error {
	fmt.Println("  Copying configuration files from active system...")

	// Use rsync to copy, but exclude certain files
	cmd := exec.Command("rsync", "-a",
		"--exclude=fstab",       // fstab is generated
		"--exclude=mtab",        // mtab is dynamic
		"--exclude=hostname",    // might be container-specific
		"--exclude=resolv.conf", // often a symlink
		srcDir+"/", dstDir+"/")

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy /etc: %w\nOutput: %s", err, string(output))
	}

	fmt.Println("  Configuration files copied")
	return nil
}

// copyFile copies a single file preserving permissions
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := srcFile.WriteTo(dstFile); err != nil {
		return err
	}

	return nil
}
