# UAPI Boot Loader Specification Compliance

## Overview

`phukit` implements the [UAPI Group Boot Loader Specification](https://uapi-group.org/specifications/specs/boot_loader_specification/) to ensure maximum compatibility with modern Linux boot infrastructure and tools.

## What is the UAPI Boot Loader Specification?

The UAPI (Userspace API) Group Boot Loader Specification is a standard that defines:

- How bootloader partitions should be structured
- Where boot loader entries should be stored
- How boot loaders should discover and present boot options
- Standard mount points for boot-related partitions

The specification is designed to enable cooperation between multiple operating systems, bootloaders, and tools on a single system.

## Key Requirements Implemented

### 1. Partition Types

We use the correct GPT partition type GUIDs:

- **ESP (EFI System Partition)**: `c12a7328-f81f-11d2-ba4b-00a0c93ec93b`

  - 2GB FAT32 partition
  - Contains EFI bootloader binaries

- **XBOOTLDR (Extended Boot Loader Partition)**: `bc13c2ff-59e6-4262-a352-b275fd6f7172`
  - 1GB ext4 partition
  - Contains kernels, initramfs, and boot loader entries

### 2. Mount Points

Per the specification's recommendations:

```
/efi     → ESP (EFI System Partition)
/boot    → XBOOTLDR (Extended Boot Loader Partition)
```

**NOT** the traditional nested structure:

```
❌ /boot/efi  → ESP (deprecated, causes autofs complications)
```

### 3. Boot Entry Location

Boot loader entries are stored in:

```
/boot/loader/entries/*.conf
```

This is on the XBOOTLDR partition (`/boot`), **not** on the ESP (`/efi`).

### 4. Kernel and Initramfs Location

Kernel and initramfs files are stored in:

```
/boot/vmlinuz-*
/boot/initramfs-*.img
```

Also on the XBOOTLDR partition (`/boot`), for both GRUB and systemd-boot.

### 5. Boot Entry Format

Boot entries follow the Type #1 format specified by UAPI:

```conf
title   Fedora Linux 40
linux   /vmlinuz-6.11.0-1.fc40.x86_64
initrd  /initramfs-6.11.0-1.fc40.x86_64.img
options root=UUID=abc123... rw systemd.mount-extra=UUID=def456...:/var:ext4:defaults
```

## Why UAPI Compliance Matters

### 1. Avoids Nested Mount Complications

The specification explicitly recommends against mounting ESP at `/boot/efi` because:

- Nested autofs mounts are complex to implement correctly
- Inner autofs triggers outer autofs, causing mount ordering issues
- systemd implements this via direct autofs mounts

### 2. Protects Data Integrity

FAT32 (used by ESP) has weak data integrity properties:

- Should remain unmounted when not needed
- Separate mounts for ESP and XBOOTLDR allow independent mount/unmount
- Reduces risk of corruption from unexpected shutdowns

### 3. Standard Tool Compatibility

UAPI-compliant layout works seamlessly with:

- `systemd-gpt-auto-generator` - Automatic partition discovery
- `bootctl` - systemd-boot management tool
- `kernel-install` - Standard kernel installation scripts
- Other UAPI-aware bootloader management tools

### 4. Multi-Boot Cooperation

The specification enables:

- Multiple OS installations sharing boot partitions
- No conflicts over boot loader ownership
- Each OS can manage its own boot entries
- Boot loaders can discover all installed systems

## Implementation Details

### Partition Creation

In `pkg/partition.go`, we create partitions with proper type codes:

```go
// ESP with type EF00 (c12a7328-f81f-11d2-ba4b-00a0c93ec93b)
{"sgdisk", "--new=1:0:+2G", "--typecode=1:EF00", "--change-name=1:EFI", device}

// XBOOTLDR with type bc13c2ff...
{"sgdisk", "--new=2:0:+1G", "--typecode=2:bc13c2ff-59e6-4262-a352-b275fd6f7172", "--change-name=2:boot", device}
```

### Mount Structure

In `pkg/partition.go`, partitions are mounted according to spec:

```go
// Mount XBOOTLDR to /boot
bootDir := filepath.Join(mountPoint, "boot")
cmd = exec.Command("mount", scheme.BootPartition, bootDir)

// Mount ESP to /efi (NOT nested under /boot)
efiDir := filepath.Join(mountPoint, "efi")
cmd = exec.Command("mount", scheme.EFIPartition, efiDir)
```

### Bootloader Configuration

#### systemd-boot

In `pkg/bootloader.go`:

```go
// Entries directory on XBOOTLDR
loaderDir := filepath.Join(b.TargetDir, "boot", "loader")
entriesDir := filepath.Join(loaderDir, "entries")

// Kernels on XBOOTLDR
bootDir := filepath.Join(b.TargetDir, "boot")
```

#### GRUB2

GRUB continues to work with its traditional locations:

- Configuration in `/boot/grub/` (on XBOOTLDR)
- Kernels in `/boot/` (on XBOOTLDR)
- EFI binaries in `/efi/EFI/BOOT/` (on ESP)

### Update Process

In `pkg/update.go`, updates follow the same structure:

```go
// Mount XBOOTLDR and ESP separately
cmd := exec.Command("mount", u.Scheme.BootPartition, u.Config.BootMountPoint)
efiMountPoint := filepath.Join(os.TempDir(), "phukit-update-efi")
cmd = exec.Command("mount", u.Scheme.EFIPartition, efiMountPoint)

// Update entries in /boot/loader/entries
loaderDir := filepath.Join(u.Config.BootMountPoint, "loader")
```

## Benefits for phukit Users

### Automatic Partition Discovery

systemd will automatically mount:

- ESP to `/efi` (based on partition type)
- XBOOTLDR to `/boot` (based on partition type)
- No manual fstab entries required for boot partitions

### Compatibility with systemd Tools

Standard systemd tools work out of the box:

```bash
bootctl status    # Shows boot configuration
bootctl list      # Lists available boot entries
```

### Future-Proof

As the UAPI specification evolves and gains wider adoption:

- phukit installations remain compatible
- New tools and features work automatically
- Migration to newer boot technologies is easier

## Testing UAPI Compliance

### Verify Partition Types

```bash
# Check partition type GUIDs
sudo sgdisk -p /dev/sdX

# Should show:
# 1: EF00 (EFI System)
# 2: bc13c2ff-... (XBOOTLDR)
```

### Verify Mount Points

```bash
# Check actual mount points
mount | grep -E '(/boot|/efi)'

# Should show:
# /dev/sdX1 on /efi type vfat ...
# /dev/sdX2 on /boot type ext4 ...
```

### Verify Boot Entry Location

```bash
# Boot entries should be in /boot/loader/entries
ls /boot/loader/entries/
# bootc.conf
# bootc-previous.conf
```

### Verify Kernel Location

```bash
# Kernels should be in /boot (not /boot/efi)
ls /boot/vmlinuz-*
ls /boot/initramfs-*.img
```

## References

- [UAPI Boot Loader Specification](https://uapi-group.org/specifications/specs/boot_loader_specification/)
- [systemd-gpt-auto-generator(8)](https://www.freedesktop.org/software/systemd/man/systemd-gpt-auto-generator.html)
- [systemd-boot(7)](https://www.freedesktop.org/software/systemd/man/systemd-boot.html)
- [bootctl(1)](https://www.freedesktop.org/software/systemd/man/bootctl.html)

## Migration Notes

### From Traditional /boot/efi Layout

If you have an existing system with ESP mounted at `/boot/efi`:

1. New installations by phukit will use `/efi` mount point
2. Existing systems continue to work (backward compatible)
3. Consider reinstalling to adopt the new layout for better tooling support

### Why We Changed

The traditional nested mount was working but:

- Not recommended by the UAPI specification
- Causes complications with modern systemd autofs
- Limits compatibility with standard tools
- Made our codebase more complex than necessary

The UAPI-compliant layout simplifies our code and improves standard compliance.
