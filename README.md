# phukit

A Go application for installing bootc compatible containers to physical disks.

## Overview

`phukit` is a command-line tool that installs bootc-compatible container images directly to physical disks. It handles the complete installation process including partitioning, filesystem creation, container extraction, and bootloader installation - all without requiring the `bootc` command itself.

## Features

- 🔍 **Disk Discovery**: List and inspect available physical disks
- ✅ **Validation**: Verify disks are suitable for installation
- 🚀 **Automated Installation**: Complete installation workflow with safety checks
- 🔧 **Kernel Arguments**: Support for custom kernel arguments
- 🏷️ **Multiple Device Types**: Supports SATA (sd*), NVMe (nvme*), virtio (vd\*), and MMC devices
- 🛡️ **Safety Features**: Confirmation prompts and dry-run mode
- 📝 **Detailed Logging**: Verbose output for troubleshooting

## Prerequisites

Before using `phukit`, ensure you have the following installed:

- **podman**: Container runtime for pulling container images
- **sgdisk**: GPT partition table manipulation tool (usually in `gdisk` package)
- **mkfs tools**: `mkfs.vfat`, `mkfs.ext4` for filesystem creation
- **GRUB2**: `grub-install` or `grub2-install` for bootloader installation
- **Root privileges**: Required for disk operations

### System Requirements

- Linux operating system
- 64-bit architecture
- Root/sudo access for disk operations

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/frostyard/phukit.git
cd phukit

# Build the binary
./build.sh

# Or use go directly
go build -o phukit .

# Install to system (optional)
sudo cp phukit /usr/local/bin/
```

### Using Docker

```bash
# Build the Docker image
docker build -t phukit .

# Run with necessary privileges
docker run --rm --privileged \
  -v /dev:/dev \
  -v /var/lib/containers:/var/lib/containers \
  phukit --help
```

## Usage

### List Available Disks

```bash
# List all available disks
phukit list

# List with verbose output
phukit list -v
```

Example output:

```
Available disks:

Device: /dev/sda
  Size:      238.5 GB (238475288576 bytes)
  Model:     Samsung SSD 850
  Removable: false
  Partitions:
    - /dev/sda1 (512.0 MB) mounted at /boot/efi
    - /dev/sda2 (237.5 GB) mounted at /

Device: /dev/nvme0n1
  Size:      1.0 TB (1000204886016 bytes)
  Model:     Samsung SSD 970 EVO
  Removable: false
  Partitions: none
```

### Validate a Disk

```bash
# Check if a disk is suitable for installation
phukit validate --device /dev/sda

# Or use device aliases
phukit validate -d /dev/disk/by-id/ata-Samsung_SSD_850
```

### Install to Disk

```bash
# Basic installation
phukit install \
  --image quay.io/centos-bootc/centos-bootc:stream9 \
  --device /dev/sda

# With custom kernel arguments
phukit install \
  --image quay.io/my-org/my-image:latest \
  --device /dev/nvme0n1 \
  --karg console=ttyS0 \
  --karg quiet

# Skip image pull (use already pulled image)
phukit install \
  --image localhost/my-custom-image \
  --device /dev/sda \
  --skip-pull

# Dry run (test without making changes)
phukit install \
  --image quay.io/example/image:latest \
  --device /dev/sda \
  --dry-run
```

### Global Flags

```bash
# Verbose output
phukit install --image IMAGE --device DEVICE -v

# Dry run mode (no actual changes)
phukit install --image IMAGE --device DEVICE --dry-run

# Use custom config file
phukit --config /path/to/config.yaml install --image IMAGE --device DEVICE
```

## Configuration

You can create a configuration file at `~/.phukit.yaml` to set default values:

```yaml
# Enable verbose output by default
verbose: false

# Enable dry-run mode by default
dry-run: false
# Default image to use
# default-image: "quay.io/example/bootc-image:latest"

# Default kernel arguments
# kernel-args:
#   - console=ttyS0
#   - quiet
```

See [.phukit.yaml.example](.phukit.yaml.example) for a complete example.

## How It Works

The installation process follows these steps:
required tools (podman, sgdisk, mkfs, grub) are available 2. **Disk Validation**: Ensures the target disk meets requirements (size, not mounted) 3. **Image Pull**: Downloads the container image using podman (unless --skip-pull is used) 4. **Confirmation**: Prompts user to confirm data destruction 5. **Disk Wipe**: Removes existing partition tables and filesystem signatures 6. **Partitioning**: Creates GPT partition table with EFI, boot, and root partitions 7. **Formatting**: Formats partitions (FAT32 for EFI, ext4 for boot and root) 8. **Extraction**: Extracts container filesystem to mounted partitions 9. **Configuration**: Creates /etc/fstab and sets up system directories
10.Installation Details

`phukit` performs a native installation without requiring the `bootc` command:

### Partitioning

Creates a GPT partition table with:

- **EFI System Partition** (512MB, FAT32): For UEFI boot files
- **Boot Partition** (1GB, ext4): For kernel and initramfs
- **Root Partition** (remaining space, ext4): For the system filesystem

### Container Extraction

Uses podman to:

1. Create a temporary container from the image
2. Export the container filesystem
3. Extract it directly to the mounted root partition

### Bootloader Configuration

Automatically detects and installs the appropriate bootloader:

- **GRUB2**: Default bootloader, widely compatible
- **systemd-boot**: Used if detected in the container

The bootloader is configured with:

- Correct root filesystem UUID
- Custom kernel arguments (if specified)
- Generated initramfs references
- Devgrub-install or grub2-install not found"

Install GRUB2:

```bash
# Fedora/RHEL/CentOS
sudo dnf install grub2-efi-x64 grub2-tools

# Ubuntu/Debian
sudo apt install grub-efi-amd64 grub2-common
```

### "sgdisk not found"

Install gdisk:

```bash
# Fedora/RHEL/CentOS
sudo dnf install gdisk

# Ubuntu/Debian
sudo apt install gdisk
```

This ensures the bootc installer has all necessary permissions to configure the target disk.

## Safety Features

- **Unmounted Check**: Refuses to install if any partition is mounted
- **Size Validation**: Ensures disk has minimum 10GB space
- **Confirmation Prompt**: Requires typing "yes" before wiping disk
- **Dry Run Mode**: Test operations without making changes
- **Verbose Logging**: Track exactly what's happening

## Troubleshooting

### "bootc is not available"

Install bootc from https://containers.github.io/bootc/

### "podman is not available"

Install podman:

```bash
# Fedora/RHEL/CentOS
sudo dnf install podman

# Ubuntu/Debian
sudo apt install podman
```

### "device does not exist"

Ensure you're using the correct device path. Use `phukit list` to see available devices.

### "partition is mounted"

Unmount all partitions before installation:

```bash
sudo umount /dev/sda1
sudo umount /dev/sda2
```

### Permission Denied

Run phukit with sudo:

```bash
sudo phukit install --image IMAGE --device DEVICE
```

## Contributing

Contributions are welcome! Please feel free to submit issues or pull requests.

## License

MIT License - see LICENSE file for details

## Acknowledgments

- [podman](https://podman.io/) - Container runtime for OCI images
- [Cobra](https://github.com/spf13/cobra) - CLI framework
- [Viper](https://github.com/spf13/viper) - Configuration management
- [GRUB2](https://www.gnu.org/software/grub/) - Bootloader

## Related Projects

- [bootc](https://github.com/containers/bootc) - Transactional, in-place operating system updates using OCI/Docker container images
- [podman](https://github.com/containers/podman) - Tool for managing OCI containers and pods
- [OSTree](https://github.com/ostreedev/ostree) - Operating system and container image management

## Warning

⚠️ **This tool will DESTROY ALL DATA on the target disk.** Always double-check the device path and ensure you have backups of any important data.
