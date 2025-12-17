# phukit

[![Tests](https://github.com/bketelsen/phukit/actions/workflows/test.yml/badge.svg)](https://github.com/bketelsen/phukit/actions/workflows/test.yml)

A Go application for installing bootc-compatible containers to physical disks with A/B partitioning and atomic updates.

## Overview

`phukit` is a command-line tool that installs bootc-compatible container images directly to physical disks. It handles the complete installation process including partitioning, filesystem creation, container extraction, and bootloader installation - all without requiring the `bootc` command itself.

The tool implements an A/B partition scheme for safe, atomic system updates with automatic rollback capability.

## Source Image Requirements

For successful installation and updates, the source container image must meet the following requirements:

### Kernel and Initramfs Location

The kernel and initramfs files **must** be located in `/usr/lib/modules/$KERNEL_VERSION/` within the container image:

- **Kernel**: `/usr/lib/modules/$KERNEL_VERSION/vmlinuz` or `/usr/lib/modules/$KERNEL_VERSION/vmlinuz-$KERNEL_VERSION`
- **Initramfs**: One of the following in the same directory:
  - `/usr/lib/modules/$KERNEL_VERSION/initramfs.img`
  - `/usr/lib/modules/$KERNEL_VERSION/initrd.img`
  - `/usr/lib/modules/$KERNEL_VERSION/initramfs-$KERNEL_VERSION.img`
  - `/usr/lib/modules/$KERNEL_VERSION/initrd.img-$KERNEL_VERSION`

During installation or update, `phukit` will automatically copy these files from `/usr/lib/modules/$KERNEL_VERSION/` to the shared `/boot` partition.

### Filesystem Structure

The container image should follow standard Linux Filesystem Hierarchy Standard (FHS):

- `/usr`: System binaries and libraries (read-only in production)
- `/etc`: Configuration files (will be managed with 3-way merge during updates)
- `/var`: Variable data (symlinked to shared partition)
- `/home`: User home directories (symlinked to `/var/home`)
- `/root`: Root user home directory (symlinked to `/var/roothome`)
- `/opt`: Optional packages
- `/srv`: Service data (symlinked to `/var/srv`)
- `/tmp`: Temporary files (mounted as tmpfs at runtime)

### Required System Components

The image should contain:

- **Linux kernel modules** in `/usr/lib/modules/$KERNEL_VERSION/`
- **System libraries** in `/usr/lib` and `/usr/lib64`
- **Essential binaries** in `/usr/bin` and `/usr/sbin`
- **Basic system configuration** in `/etc`

### Optional but Recommended

- **systemd**: For service management and system initialization
- **NetworkManager** or similar for network configuration
- **SSH server**: For remote access
- **Package manager**: For installing additional software after deployment

### Example Image Structure

```
/
├── usr/
│   ├── bin/
│   ├── lib/
│   ├── lib64/
│   └── lib/modules/
│       └── 6.11.0-1.el9.x86_64/
│           ├── vmlinuz
│           ├── initramfs.img
│           └── kernel/
├── etc/
│   ├── fstab
│   ├── hostname
│   └── systemd/
├── var/  (will be symlinked to shared partition)
├── home/  (will be symlinked to /var/home)
└── root/  (will be symlinked to /var/roothome)
```

### Building Compatible Images

To create a compatible bootc image, ensure your Containerfile/Dockerfile includes kernel installation:

```dockerfile
FROM quay.io/centos/centos:stream9

# Install kernel and other packages
RUN dnf install -y kernel kernel-modules initramfs-tools

# Kernel and initramfs will be in /usr/lib/modules/$(uname -r)/
# No need to manually move them - phukit handles the extraction
```

## Features

- 🔍 **Disk Discovery**: List and inspect available physical disks
- ✅ **Validation**: Verify disks are suitable for installation
- 🚀 **Automated Installation**: Complete installation workflow with safety checks
- 🔄 **A/B Updates**: Dual root partition system for safe, atomic updates with rollback
- 🔧 **Kernel Arguments**: Support for custom kernel arguments
- 💾 **/etc Persistence**: Three-way merge preserves user configuration across updates
- 🏷️ **Multiple Device Types**: Supports SATA (sd\*), NVMe (nvme\*), virtio (vd\*), and MMC devices
- 🛡️ **Safety Features**: Confirmation prompts and force flag for automation
- 📝 **Detailed Logging**: Verbose output for troubleshooting
- 🔐 **Configuration Storage**: Stores image reference for easy updates

## Prerequisites

Before using `phukit`, ensure you have the following installed:

- **podman**: Container runtime for pulling container images
- **sgdisk**: GPT partition table manipulation tool (usually in `gdisk` package)
- **mkfs tools**: `mkfs.vfat`, `mkfs.ext4` for filesystem creation
- **GRUB2**: `grub-install` or `grub2-install` for bootloader installation
- **Root privileges**: Required for disk operations

### System Requirements

- Linux operating system (tested on Fedora, Ubuntu, CentOS Stream)
- x86_64 or ARM64 architecture
- Root/sudo access for disk operations
- Minimum 50GB disk space (43GB for system partitions + space for /var)

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/bketelsen/phukit.git
cd phukit

# Build the binary
make build

# Install to system (optional)
sudo make install
```

### From Release

Download the latest release from [GitHub Releases](https://github.com/bketelsen/phukit/releases):

```bash
# Download for your architecture
curl -LO https://github.com/bketelsen/phukit/releases/latest/download/phukit-linux-amd64

# Make executable
chmod +x phukit-linux-amd64

# Install
sudo mv phukit-linux-amd64 /usr/local/bin/phukit
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

# Skip confirmation prompt (for automation)
phukit install \
  --image quay.io/example/image:latest \
  --device /dev/sda \
  --force

# Dry run (test without making changes)
phukit install \
  --image quay.io/example/image:latest \
  --device /dev/sda \
  --dry-run
```

### Update System

The A/B update system allows you to safely update your system by installing to an inactive root partition:

```bash
# Update to latest version of the installed image
phukit update --device /dev/sda

# Update to a specific image
phukit update \
  --image quay.io/my-org/my-image:v2.0 \
  --device /dev/sda

# Skip pulling (use already pulled image)
phukit update \
  --image localhost/my-image:latest \
  --device /dev/sda \
  --skip-pull

# Force update without confirmation
phukit update \
  --device /dev/sda \
  --force

# Add custom kernel arguments for the new system
phukit update \
  --device /dev/sda \
  --karg console=ttyS0 \
  --karg debug
```

After update, reboot to activate the new system. The previous version remains available in the boot menu for rollback.

### Global Flags

```bash
# Verbose output
phukit install --image IMAGE --device DEVICE -v

# Dry run mode (no actual changes)
phukit install --image IMAGE --device DEVICE --dry-run
```

## How It Works

`phukit` performs a native installation without requiring the `bootc` command. The system is designed with A/B partitioning for safe, atomic updates.

### A/B Partitioning Scheme

`phukit` creates a GPT partition table with dual root partitions for atomic updates:

1. **EFI System Partition** (2GB, FAT32): UEFI boot files and bootloader
2. **Boot Partition** (1GB, ext4): Shared kernel and initramfs files
3. **Root Partition 1** (20GB, ext4): First root filesystem (OS A)
4. **Root Partition 2** (20GB, ext4): Second root filesystem (OS B)
5. **Var Partition** (remaining space, ext4): Shared `/var` for both systems

This layout enables:

- **Atomic Updates**: Install new version to inactive partition without affecting running system
- **Safe Rollback**: Previous system remains bootable via GRUB menu
- **Shared Data**: `/var` partition shared between both systems for persistent data
- **Zero Downtime**: Switch between versions with a simple reboot

### Installation Process

The initial installation follows these steps:

1. **Prerequisites Check**: Verifies required tools (podman, sgdisk, mkfs, grub) are available
2. **Disk Validation**: Ensures the target disk meets requirements (size, not mounted)
3. **Image Pull**: Downloads the container image using podman (unless `--skip-pull` is used)
4. **Confirmation**: Prompts user to confirm data destruction (unless `--force` is used)
5. **Disk Wipe**: Removes existing partition tables and filesystem signatures
6. **Partitioning**: Creates the 5-partition GPT layout
7. **Formatting**: Formats all partitions (FAT32 for EFI, ext4 for others)
8. **Mounting**: Mounts partitions in correct order for extraction
9. **Extraction**: Extracts container filesystem to Root Partition 1
10. **System Setup**: Creates `/var` structure, saves pristine `/etc`
11. **Configuration**: Creates `/etc/fstab`, `/etc/phukit/config.json`
12. **Bootloader Installation**: Installs and configures GRUB2 with UUIDs

### Update Process

Updates use the inactive root partition for safe atomic updates:

1. **Active Detection**: Determines which root partition is currently booted
2. **Target Selection**: Selects the inactive partition as update target
3. **Image Pull**: Downloads the new container image (unless `--skip-pull` is used)
4. **Mounting**: Mounts target partition and boot partition
5. **Clearing**: Removes old content from target partition
6. **Extraction**: Extracts new filesystem to target partition
7. **/etc Merge**: Performs 3-way merge to preserve user configuration
8. **System Directories**: Sets up necessary system directories
9. **Bootloader Update**: Updates GRUB to boot from new partition by default
10. **Dual Boot Menu**: Creates menu entries for both updated and previous systems

After reboot, the system boots from the new partition. The old partition remains available for rollback via the GRUB menu.

### /etc Configuration Persistence

`phukit` implements a 3-way merge for `/etc` configuration during updates:

1. **Pristine**: Original `/etc` from initial installation (stored in `/var/lib/phukit/etc.pristine`)
2. **Active**: Current `/etc` with user modifications
3. **New**: Fresh `/etc` from new container image

The merge algorithm:

- Files modified by user in active system → **preserved** in new system
- Files unchanged from pristine → **updated** from new container
- New files in container → **added** to new system
- Files deleted by user → **remain deleted** in new system

This ensures user configuration survives updates while allowing package updates to deliver new defaults.

## System Configuration

After installation, `phukit` writes a configuration file to `/etc/phukit/config.json`:

```json
{
  "image_ref": "quay.io/example/bootc-image:latest",
  "device": "/dev/sda",
  "install_date": "2025-12-16T10:30:00Z",
  "kernel_args": ["console=ttyS0", "quiet"],
  "bootloader_type": "grub2",
  "active_partition": "/dev/sda3"
}
```

This configuration is automatically used during updates, so you don't need to specify the image reference again.

## Configuration File

Create `~/.phukit.yaml` for user defaults:

```yaml
# Enable verbose logging
verbose: false

# Enable dry-run mode by default
dry-run: false
# Default kernel arguments
# kernel-args:
#   - console=ttyS0
#   - quiet
```

See [.phukit.yaml.example](.phukit.yaml.example) for a complete example.

## Safety Features

- **Unmounted Check**: Refuses to install if any partition is mounted
- **Size Validation**: Ensures disk has minimum 50GB space
- **Confirmation Prompt**: Requires typing "yes" before wiping disk (unless `--force`)
- **Dry Run Mode**: Test operations without making changes
- **Verbose Logging**: Track exactly what's happening
- **A/B Rollback**: Previous system always available in boot menu

## Troubleshooting

### "grub-install or grub2-install not found"

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
# etc...
```

### Permission Denied

Run phukit with sudo:

```bash
sudo phukit install --image IMAGE --device DEVICE
```

## Documentation

- [A/B Updates](docs/AB-UPDATES.md) - Detailed documentation on the A/B update system
- [Incus Integration Tests](docs/INCUS-TESTS.md) - VM-based testing documentation
- [Implementation Details](IMPLEMENTATION.md) - Technical implementation details

## Testing

### Unit Tests

```bash
# Run unit tests (no root required)
make test-unit

# Run linter
make lint
```

### Integration Tests

Integration tests require root privileges to perform disk operations:

```bash
# Run basic integration tests (loop devices)
sudo make test-integration

# Run bootc installation tests
sudo make test-install

# Run A/B update tests
sudo make test-update
```

### Incus VM Tests

For comprehensive end-to-end testing in isolated virtual machines:

```bash
# Install Incus first: https://linuxcontainers.org/incus/docs/main/installing/
# Initialize Incus: incus admin init

# Run full integration tests in Incus VM
sudo make test-incus
```

The Incus test suite:

- Creates an isolated VM with dedicated virtual disk
- Tests complete installation workflow
- Verifies partition layout and bootloader
- Tests A/B update functionality
- Validates kernel/initramfs installation
- Checks GRUB configuration for both boot options
- Automatically cleans up all resources

**Note**: Incus tests take 10-20 minutes depending on network speed and system performance.

## Contributing

Contributions are welcome! Please feel free to submit issues or pull requests.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

MIT License - see [LICENSE](LICENSE) file for details.

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

⚠️ **THIS TOOL WILL DESTROY ALL DATA ON THE TARGET DISK** ⚠️

Always double-check the device path before running install commands. Use `--dry-run` to test without making changes.

## Missing / Planned Features

* someone ought to actually test this
* root mount RO
* export container as squashfs/erofs/similar, mount that instead of fs copy
