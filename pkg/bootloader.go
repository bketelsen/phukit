package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BootloaderType represents the type of bootloader to install
type BootloaderType string

const (
	BootloaderGRUB2       BootloaderType = "grub2"
	BootloaderSystemdBoot BootloaderType = "systemd-boot"
)

// BootloaderInstaller handles bootloader installation
type BootloaderInstaller struct {
	Type       BootloaderType
	TargetDir  string
	Device     string
	Scheme     *PartitionScheme
	KernelArgs []string
	Verbose    bool
}

// NewBootloaderInstaller creates a new BootloaderInstaller
func NewBootloaderInstaller(targetDir, device string, scheme *PartitionScheme) *BootloaderInstaller {
	return &BootloaderInstaller{
		Type:       BootloaderGRUB2, // Default to GRUB2
		TargetDir:  targetDir,
		Device:     device,
		Scheme:     scheme,
		KernelArgs: []string{},
	}
}

// SetType sets the bootloader type
func (b *BootloaderInstaller) SetType(t BootloaderType) {
	b.Type = t
}

// AddKernelArg adds a kernel argument
func (b *BootloaderInstaller) AddKernelArg(arg string) {
	b.KernelArgs = append(b.KernelArgs, arg)
}

// SetVerbose enables verbose output
func (b *BootloaderInstaller) SetVerbose(verbose bool) {
	b.Verbose = verbose
}

// Install installs the bootloader
func (b *BootloaderInstaller) Install() error {
	fmt.Printf("Installing %s bootloader...\n", b.Type)

	switch b.Type {
	case BootloaderGRUB2:
		return b.installGRUB2()
	case BootloaderSystemdBoot:
		return b.installSystemdBoot()
	default:
		return fmt.Errorf("unsupported bootloader type: %s", b.Type)
	}
}

// installGRUB2 installs GRUB2 bootloader
func (b *BootloaderInstaller) installGRUB2() error {
	fmt.Println("  Installing GRUB2...")

	// Check if grub-install is available
	grubInstallCmd := "grub-install"
	if _, err := exec.LookPath("grub2-install"); err == nil {
		grubInstallCmd = "grub2-install"
	}

	// Install GRUB to the disk
	args := []string{
		"--target=x86_64-efi",
		"--efi-directory=" + filepath.Join(b.TargetDir, "boot", "efi"),
		"--boot-directory=" + filepath.Join(b.TargetDir, "boot"),
		"--bootloader-id=BOOT",
		"--removable", // Install to removable media path for compatibility
	}

	if b.Verbose {
		args = append(args, "--verbose")
	}

	cmd := exec.Command(grubInstallCmd, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install GRUB: %w", err)
	}

	// Generate GRUB configuration
	if err := b.generateGRUBConfig(); err != nil {
		return fmt.Errorf("failed to generate GRUB config: %w", err)
	}

	fmt.Println("  GRUB2 installation complete")
	return nil
}

// generateGRUBConfig generates GRUB configuration
func (b *BootloaderInstaller) generateGRUBConfig() error {
	fmt.Println("  Generating GRUB configuration...")

	// Get root UUID
	rootUUID, err := GetPartitionUUID(b.Scheme.Root1Partition)
	if err != nil {
		return fmt.Errorf("failed to get root UUID: %w", err)
	}

	// Find kernel and initramfs
	bootDir := filepath.Join(b.TargetDir, "boot")
	kernels, err := filepath.Glob(filepath.Join(bootDir, "vmlinuz-*"))
	if err != nil || len(kernels) == 0 {
		return fmt.Errorf("no kernel found in /boot")
	}
	kernel := filepath.Base(kernels[0])
	kernelVersion := strings.TrimPrefix(kernel, "vmlinuz-")

	// Look for initramfs/initrd
	var initrd string
	initrdPatterns := []string{
		filepath.Join(bootDir, "initramfs-"+kernelVersion+".img"),
		filepath.Join(bootDir, "initrd.img-"+kernelVersion),
		filepath.Join(bootDir, "initramfs-"+kernelVersion),
	}
	for _, pattern := range initrdPatterns {
		if _, err := os.Stat(pattern); err == nil {
			initrd = filepath.Base(pattern)
			break
		}
	}

	// Build kernel command line
	kernelCmdline := []string{
		"root=UUID=" + rootUUID,
		"ro",
		"console=tty0",
	}
	kernelCmdline = append(kernelCmdline, b.KernelArgs...)

	// Create GRUB config
	grubCfg := fmt.Sprintf(`set timeout=5
set default=0

menuentry 'Linux' {
    linux /vmlinuz-%s %s
    initrd /%s
}
`, kernelVersion, strings.Join(kernelCmdline, " "), initrd)

	// Write GRUB config
	grubDir := filepath.Join(b.TargetDir, "boot", "grub")
	if _, err := os.Stat(grubDir); os.IsNotExist(err) {
		grubDir = filepath.Join(b.TargetDir, "boot", "grub2")
	}

	if err := os.MkdirAll(grubDir, 0755); err != nil {
		return fmt.Errorf("failed to create grub directory: %w", err)
	}

	grubCfgPath := filepath.Join(grubDir, "grub.cfg")
	if err := os.WriteFile(grubCfgPath, []byte(grubCfg), 0644); err != nil {
		return fmt.Errorf("failed to write grub.cfg: %w", err)
	}

	fmt.Printf("  Created GRUB configuration at %s\n", grubCfgPath)
	return nil
}

// installSystemdBoot installs systemd-boot bootloader
func (b *BootloaderInstaller) installSystemdBoot() error {
	fmt.Println("  Installing systemd-boot...")

	// Check if bootctl is available
	if _, err := exec.LookPath("bootctl"); err != nil {
		return fmt.Errorf("bootctl not found, systemd-boot requires systemd")
	}

	// Install systemd-boot
	cmd := exec.Command("bootctl", "--path="+filepath.Join(b.TargetDir, "boot", "efi"), "install")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install systemd-boot: %w", err)
	}

	// Generate loader configuration
	if err := b.generateSystemdBootConfig(); err != nil {
		return fmt.Errorf("failed to generate systemd-boot config: %w", err)
	}

	fmt.Println("  systemd-boot installation complete")
	return nil
}

// generateSystemdBootConfig generates systemd-boot configuration
func (b *BootloaderInstaller) generateSystemdBootConfig() error {
	fmt.Println("  Generating systemd-boot configuration...")

	// Get root UUID
	rootUUID, err := GetPartitionUUID(b.Scheme.Root1Partition)
	if err != nil {
		return fmt.Errorf("failed to get root UUID: %w", err)
	}

	// Find kernel
	bootDir := filepath.Join(b.TargetDir, "boot")
	kernels, err := filepath.Glob(filepath.Join(bootDir, "vmlinuz-*"))
	if err != nil || len(kernels) == 0 {
		return fmt.Errorf("no kernel found in /boot")
	}
	kernel := filepath.Base(kernels[0])
	kernelVersion := strings.TrimPrefix(kernel, "vmlinuz-")

	// Look for initramfs
	var initrd string
	initrdPatterns := []string{
		filepath.Join(bootDir, "initramfs-"+kernelVersion+".img"),
		filepath.Join(bootDir, "initrd.img-"+kernelVersion),
		filepath.Join(bootDir, "initramfs-"+kernelVersion),
	}
	for _, pattern := range initrdPatterns {
		if _, err := os.Stat(pattern); err == nil {
			initrd = filepath.Base(pattern)
			break
		}
	}

	// Build kernel command line
	kernelCmdline := []string{
		"root=UUID=" + rootUUID,
		"ro",
	}
	kernelCmdline = append(kernelCmdline, b.KernelArgs...)

	// Create loader configuration
	loaderDir := filepath.Join(b.TargetDir, "boot", "efi", "loader")
	if err := os.MkdirAll(loaderDir, 0755); err != nil {
		return fmt.Errorf("failed to create loader directory: %w", err)
	}

	loaderConf := `default bootc
timeout 5
console-mode max
editor no
`
	loaderConfPath := filepath.Join(loaderDir, "loader.conf")
	if err := os.WriteFile(loaderConfPath, []byte(loaderConf), 0644); err != nil {
		return fmt.Errorf("failed to write loader.conf: %w", err)
	}

	// Create boot entry
	entriesDir := filepath.Join(loaderDir, "entries")
	if err := os.MkdirAll(entriesDir, 0755); err != nil {
		return fmt.Errorf("failed to create entries directory: %w", err)
	}

	entry := fmt.Sprintf(`title   Linux
linux   /vmlinuz-%s
initrd  /%s
options %s
`, kernelVersion, initrd, strings.Join(kernelCmdline, " "))

	entryPath := filepath.Join(entriesDir, "bootc.conf")
	if err := os.WriteFile(entryPath, []byte(entry), 0644); err != nil {
		return fmt.Errorf("failed to write boot entry: %w", err)
	}

	fmt.Println("  Created systemd-boot configuration")
	return nil
}

// DetectBootloader detects which bootloader should be used based on the container
func DetectBootloader(targetDir string) BootloaderType {
	// Check if systemd-boot is preferred (presence of bootctl in container)
	if _, err := os.Stat(filepath.Join(targetDir, "usr", "bin", "bootctl")); err == nil {
		return BootloaderSystemdBoot
	}

	// Default to GRUB2
	return BootloaderGRUB2
}
