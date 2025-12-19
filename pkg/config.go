package pkg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// SystemConfigDir is the directory for phukit system configuration
	SystemConfigDir = "/etc/phukit"
	// SystemConfigFile is the main configuration file
	SystemConfigFile = "/etc/phukit/config.json"
)

// SystemConfig represents the system configuration stored in /etc/phukit/
type SystemConfig struct {
	ImageRef       string   `json:"image_ref"`       // Container image reference
	ImageDigest    string   `json:"image_digest"`    // Container image digest (sha256:...)
	Device         string   `json:"device"`          // Installation device
	InstallDate    string   `json:"install_date"`    // Installation timestamp
	KernelArgs     []string `json:"kernel_args"`     // Custom kernel arguments
	BootloaderType string   `json:"bootloader_type"` // Bootloader type (grub2, systemd-boot)
}

// WriteSystemConfig writes system configuration to /etc/phukit/config.json
func WriteSystemConfig(config *SystemConfig, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would write config to %s\n", SystemConfigFile)
		return nil
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(SystemConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal config to JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(SystemConfigFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("  Wrote system configuration to %s\n", SystemConfigFile)
	return nil
}

// ReadSystemConfig reads system configuration from /etc/phukit/config.json
func ReadSystemConfig() (*SystemConfig, error) {
	data, err := os.ReadFile(SystemConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("system configuration not found at %s (system may not be installed with phukit)", SystemConfigFile)
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config SystemConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// WriteSystemConfigToTarget writes system configuration to the target root filesystem
func WriteSystemConfigToTarget(targetDir string, config *SystemConfig, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would write config to %s/etc/phukit/config.json\n", targetDir)
		return nil
	}

	configDir := filepath.Join(targetDir, "etc", "phukit")
	configFile := filepath.Join(configDir, "config.json")

	// Create directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory in target: %w", err)
	}

	// Marshal config to JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(configFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("  Wrote system configuration to target filesystem\n")
	return nil
}

// UpdateSystemConfigImageRef updates the image reference and digest in the system config
func UpdateSystemConfigImageRef(imageRef, imageDigest string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would update config with image: %s (digest: %s)\n", imageRef, imageDigest)
		return nil
	}

	// Read existing config
	config, err := ReadSystemConfig()
	if err != nil {
		return err
	}

	// Update image reference and digest
	config.ImageRef = imageRef
	config.ImageDigest = imageDigest

	// Write back
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(SystemConfigFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("  Updated system configuration with new image: %s\n", imageRef)
	if imageDigest != "" {
		fmt.Printf("  Digest: %s\n", imageDigest)
	}
	return nil
}
