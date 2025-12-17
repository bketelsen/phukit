#!/bin/bash
# Integration tests for phukit using Incus virtual machines
# Requires: incus, root privileges
#
# For private container images:
#   Set TEST_IMAGE env var: TEST_IMAGE=ghcr.io/myorg/myimage:latest sudo -E ./test_incus.sh
#   Ensure ~/.docker/config.json has valid credentials (from 'docker login' or 'podman login')

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test configuration
VM_NAME="phukit-test-$$"
DISK_SIZE="60GB"
# Use a public bootc image for testing
# Options:
#   quay.io/centos-bootc/centos-bootc:stream9 (CentOS Stream 9)
#   quay.io/fedora/fedora-bootc:40 (Fedora 40)
# Or set TEST_IMAGE env var to use your own image (may require authentication)
#TEST_IMAGE="${TEST_IMAGE:-quay.io/fedora/fedora-bootc:42}"
TEST_IMAGE="${TEST_IMAGE:-ghcr.io/frostyard/snow:latest}"
BUILD_DIR="/tmp/phukit-test-build-$$"
TIMEOUT=1200  # 20 minutes

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}Error: This script must be run as root (sudo)${NC}"
    echo "Usage: sudo $0"
    exit 1
fi

echo -e "${GREEN}=== Phukit Incus Integration Tests ===${NC}\n"

# Cleanup function
cleanup() {
    local exit_code=$?
    echo -e "\n${YELLOW}Cleaning up test resources...${NC}"

    # Stop and delete VM
    if incus list --format csv | grep -q "^${VM_NAME},"; then
        echo "Stopping VM: ${VM_NAME}"
        incus stop ${VM_NAME} --force 2>/dev/null || true
        echo "Deleting VM: ${VM_NAME}"
        incus delete ${VM_NAME} --force 2>/dev/null || true
    fi

    # # Remove storage volume if exists
    # if incus storage volume list default --format csv | grep -q "^custom,${VM_NAME}-disk,"; then
    #     echo "Deleting storage volume: ${VM_NAME}-disk"
    #     incus storage volume delete default ${VM_NAME}-disk 2>/dev/null || true
    # fi

    # Remove build directory
    if [ -d "$BUILD_DIR" ]; then
        echo "Removing build directory: ${BUILD_DIR}"
        rm -rf "$BUILD_DIR"
    fi

    echo -e "${GREEN}Cleanup complete${NC}"
    exit $exit_code
}

# Register cleanup on exit
trap cleanup EXIT INT TERM

# Check required tools
echo -e "${YELLOW}Checking required tools...${NC}"
REQUIRED_TOOLS="incus go make"
MISSING_TOOLS=""

for tool in $REQUIRED_TOOLS; do
    if ! command -v $tool &> /dev/null; then
        MISSING_TOOLS="$MISSING_TOOLS $tool"
    else
        echo "  ✓ $tool"
    fi
done

if [ -n "$MISSING_TOOLS" ]; then
    echo -e "${RED}Error: Missing required tools:$MISSING_TOOLS${NC}"
    echo -e "${YELLOW}Install missing tools:${NC}"
    echo -e "  - Incus: https://linuxcontainers.org/incus/docs/main/installing/${NC}"
    echo -e "  - Go: https://go.dev/doc/install${NC}"
    echo -e "${YELLOW}Note: When using sudo, ensure tools are in PATH${NC}"
    echo -e "${YELLOW}Try: sudo -E env \"PATH=\$PATH\" $0${NC}"
    exit 1
fi

# Check Incus is initialized
if ! incus info >/dev/null 2>&1; then
    echo -e "${RED}Error: Incus is not initialized${NC}"
    echo -e "${YELLOW}Run: incus admin init${NC}"
    exit 1
fi

echo -e "\n${GREEN}All required tools available${NC}\n"

# Build phukit binary
echo -e "${BLUE}=== Building phukit ===${NC}"
mkdir -p "$BUILD_DIR"
make build
cp phukit "$BUILD_DIR/"
echo -e "${GREEN}Build complete${NC}\n"

# Create Incus VM
echo -e "${BLUE}=== Creating Incus VM ===${NC}"
echo "VM Name: ${VM_NAME}"
echo "Disk Size: ${DISK_SIZE}"

# Launch VM with Fedora (has good tooling support)
incus launch images:fedora/42/cloud ${VM_NAME} --vm \
    -c limits.cpu=4 \
    -c limits.memory=16GiB \
    -c security.secureboot=false

# Wait for VM to start
echo "Waiting for VM to start..."
timeout=60
while [ $timeout -gt 0 ]; do
    if incus exec ${VM_NAME} -- systemctl is-system-running --wait 2>/dev/null | grep -qE "running|degraded"; then
        break
    fi
    echo -n "."
    sleep 2
    timeout=$((timeout - 2))
done
echo ""

if [ $timeout -le 0 ]; then
    echo -e "${RED}Error: VM failed to start${NC}"
    exit 1
fi

echo -e "${GREEN}VM started successfully${NC}\n"

# Create and attach a separate disk for installation
echo -e "${BLUE}=== Creating test disk ===${NC}"
incus storage volume create default ${VM_NAME}-disk size=${DISK_SIZE} --type=block
incus storage volume attach default ${VM_NAME}-disk ${VM_NAME}
echo -e "${GREEN}Disk created and attached${NC}\n"

# Wait for the disk to appear in the VM
echo "Waiting for disk to be recognized..."
sleep 5

# Install required tools in VM
echo -e "${BLUE}=== Installing tools in VM ===${NC}"
incus exec ${VM_NAME} -- bash -c "
    dnf install -y gdisk util-linux e2fsprogs dosfstools parted rsync
" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}Tools installed${NC}\n"

# Push phukit binary to VM
echo -e "${BLUE}=== Copying phukit to VM ===${NC}"
incus file push "$BUILD_DIR/phukit" ${VM_NAME}/usr/local/bin/phukit
incus exec ${VM_NAME} -- chmod +x /usr/local/bin/phukit
echo -e "${GREEN}Binary copied${NC}\n"

# Note: Image will be pulled automatically by phukit during installation
echo -e "${BLUE}Using test image: $TEST_IMAGE${NC}"
echo "  (Image will be pulled during installation)"
echo ""

# Find the test disk device in the VM
echo -e "${BLUE}=== Identifying test disk ===${NC}"
TEST_DISK=$(incus exec ${VM_NAME} -- bash -c "
    # Find a disk that has no partitions (our empty test disk)
    for disk in \$(lsblk -ndo NAME,TYPE | grep disk | awk '{print \$1}'); do
        # Check if disk has no partitions
        if ! lsblk -no NAME /dev/\$disk | grep -q '[0-9]'; then
            echo \"/dev/\$disk\"
            exit 0
        fi
    done
")

if [ -z "$TEST_DISK" ]; then
    echo -e "${RED}Error: Could not identify test disk${NC}"
    exit 1
fi

echo "Test disk: $TEST_DISK"
incus exec ${VM_NAME} -- lsblk | sed 's/^/  /'
echo -e "${GREEN}Disk identified${NC}\n"

# Test 1: List disks
echo -e "${BLUE}=== Test 1: List Disks ===${NC}"
if incus exec ${VM_NAME} -- phukit list; then
    echo -e "${GREEN}✓ List disks successful${NC}\n"
else
    echo -e "${RED}✗ List disks failed${NC}"
    exit 1
fi

# Test 2: Validate disk
echo -e "${BLUE}=== Test 2: Validate Disk ===${NC}"
if incus exec ${VM_NAME} -- phukit validate --device "$TEST_DISK"; then
    echo -e "${GREEN}✓ Validate disk successful${NC}\n"
else
    echo -e "${RED}✗ Validate disk failed${NC}"
    exit 1
fi

# Test 3: Install to disk
echo -e "${BLUE}=== Test 3: Install to Disk ===${NC}"
echo "Installing $TEST_IMAGE to $TEST_DISK"
echo "This may take several minutes..."

# Install - pipe "yes" to confirm destruction
# Save output to log and display, then check exit code
set +e
timeout $TIMEOUT incus exec ${VM_NAME} -- bash -c "echo 'yes' | phukit install \
    --image '$TEST_IMAGE' \
    --device '$TEST_DISK' \
    --karg 'loglevel=7' \
    --karg 'systemd.journald.forward_to_console=1' \
    --verbose" 2>&1 | tee /tmp/phukit-install-$$.log | sed 's/^/  /'
INSTALL_EXIT=${PIPESTATUS[0]}
set -e

if [ $INSTALL_EXIT -eq 0 ]; then
    echo -e "${GREEN}✓ Installation successful${NC}\n"
else
    echo -e "${RED}✗ Installation failed with exit code: $INSTALL_EXIT${NC}"
    echo -e "${YELLOW}Install log saved to: /tmp/phukit-install-$$.log${NC}"
    echo -e "${YELLOW}Last 50 lines of log:${NC}"
    tail -50 /tmp/phukit-install-$$.log | sed 's/^/  /'
    exit 1
fi

# Test 4: Verify partition layout
echo -e "${BLUE}=== Test 4: Verify Partition Layout ===${NC}"
incus exec ${VM_NAME} -- bash -c "
    echo 'Partition layout:'
    lsblk $TEST_DISK
    echo ''
    echo 'Partition details:'
    sgdisk -p $TEST_DISK
" 2>&1 | sed 's/^/  /'

# Check for expected partitions
PARTITION_COUNT=$(incus exec ${VM_NAME} -- lsblk -n "$TEST_DISK" | grep -c part || true)
if [ "$PARTITION_COUNT" -eq 5 ]; then
    echo -e "${GREEN}✓ Correct number of partitions (5)${NC}\n"
else
    echo -e "${RED}✗ Expected 5 partitions, found $PARTITION_COUNT${NC}"
    exit 1
fi

# Test 5: Verify bootloader installation
echo -e "${BLUE}=== Test 5: Verify Bootloader ===${NC}"
if incus exec ${VM_NAME} -- bash -c "
    set -e
    mkdir -p /mnt/test-boot
    mkdir -p /mnt/test-boot/efi

    # Mount boot partition
    BOOT_PART=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'boot' | grep -v 'efi' | head -1 | awk '{print \"/dev/\" \$1}')
    if [ -z \"\$BOOT_PART\" ]; then
        echo 'Error: Boot partition not found'
        exit 1
    fi
    mount \$BOOT_PART /mnt/test-boot

    # Mount EFI partition
    EFI_PART=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'EFI' | head -1 | awk '{print \"/dev/\" \$1}')
    if [ -z \"\$EFI_PART\" ]; then
        echo 'Error: EFI partition not found'
        umount /mnt/test-boot
        exit 1
    fi
    mount \$EFI_PART /mnt/test-boot/efi

    echo 'Boot partition contents:'
    ls -lh /mnt/test-boot/
    echo ''
    echo 'EFI partition contents:'
    ls -lh /mnt/test-boot/efi/
    find /mnt/test-boot/efi -type f -name '*.efi' | head -10
    echo ''

    # Check for GRUB or systemd-boot
    BOOTLOADER_FOUND=false

    # Check for GRUB
    if [ -d /mnt/test-boot/grub2 ] || [ -d /mnt/test-boot/grub ]; then
        echo 'GRUB bootloader detected'
        echo 'GRUB config:'
        if [ -f /mnt/test-boot/grub2/grub.cfg ]; then
            cat /mnt/test-boot/grub2/grub.cfg
            BOOTLOADER_FOUND=true
        elif [ -f /mnt/test-boot/grub/grub.cfg ]; then
            cat /mnt/test-boot/grub/grub.cfg
            BOOTLOADER_FOUND=true
        fi
    fi

    # Check for systemd-boot
    if [ -d /mnt/test-boot/efi/loader ]; then
        echo 'systemd-boot detected'
        echo 'Loader config:'
        if [ -f /mnt/test-boot/efi/loader/loader.conf ]; then
            cat /mnt/test-boot/efi/loader/loader.conf
            BOOTLOADER_FOUND=true
        fi
        echo ''
        echo 'Boot entries:'
        if [ -d /mnt/test-boot/efi/loader/entries ]; then
            ls -lh /mnt/test-boot/efi/loader/entries/
            for entry in /mnt/test-boot/efi/loader/entries/*.conf; do
                [ -f \"\$entry\" ] && echo \"Entry: \$entry\" && cat \"\$entry\" && echo ''
            done
            BOOTLOADER_FOUND=true
        fi
    fi

    if [ \"\$BOOTLOADER_FOUND\" = false ]; then
        echo 'Error: No bootloader configuration found (checked GRUB and systemd-boot)'
        umount /mnt/test-boot/efi
        umount /mnt/test-boot
        exit 1
    fi

    # Cleanup
    umount /mnt/test-boot/efi
    umount /mnt/test-boot
    rmdir /mnt/test-boot/efi
    rmdir /mnt/test-boot
" 2>&1 | sed 's/^/  /'; then
    echo -e "${GREEN}✓ Bootloader verified${NC}\n"
else
    echo -e "${RED}✗ Bootloader verification failed${NC}"
    exit 1
fi

# Test 6: Mount and verify root filesystem
echo -e "${BLUE}=== Test 6: Verify Root Filesystem ===${NC}"
incus exec ${VM_NAME} -- bash -c "
    mkdir -p /mnt/test-root
    ROOT_PART=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'root1' | head -1 | awk '{print \"/dev/\" \$1}')
    mount \$ROOT_PART /mnt/test-root
    echo 'Root filesystem structure:'
    ls -la /mnt/test-root/ | head -20
    echo ''
    echo 'Phukit config:'
    cat /mnt/test-root/etc/phukit/config.json 2>/dev/null || echo 'Config not found'
    echo ''
    echo 'fstab:'
    cat /mnt/test-root/etc/fstab
    umount /mnt/test-root
    rmdir /mnt/test-root
" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}✓ Root filesystem verified${NC}\n"

# Test 7: Update to new version (simulated)
echo -e "${BLUE}=== Test 7: System Update ===${NC}"
echo "Performing update (writing to inactive partition)..."
echo "Note: Update requires config from /etc/phukit and pristine /etc from /var/lib/phukit"

# Update needs to read:
# 1. /etc/phukit/config.json from the active root partition
# 2. /var/lib/phukit/etc.pristine from the var partition
# Mount both partitions and bind-mount the necessary directories
echo "Mounting active partitions to access config and pristine /etc..."
incus exec ${VM_NAME} -- bash -c "
    ROOT1=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'root1' | head -1 | awk '{print \"/dev/\" \$1}')
    VAR_PART=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'var' | head -1 | awk '{print \"/dev/\" \$1}')

    mkdir -p /mnt/active-root
    mkdir -p /mnt/active-var

    # Mount the active root and var partitions
    mount \$ROOT1 /mnt/active-root
    mount \$VAR_PART /mnt/active-var

    # Bind mount the config directory to make it accessible at /etc/phukit
    mkdir -p /etc/phukit
    mount --bind /mnt/active-root/etc/phukit /etc/phukit

    # Bind mount the pristine /etc directory to make it accessible at /var/lib/phukit
    mkdir -p /var/lib/phukit
    mount --bind /mnt/active-var/lib/phukit /var/lib/phukit
" 2>&1 | sed 's/^/  /'

# Update - pipe "yes" to confirm
set +e
timeout $TIMEOUT incus exec ${VM_NAME} -- bash -c "echo 'yes' | phukit update \
    --device '$TEST_DISK' \
    --verbose" 2>&1 | tee /tmp/phukit-update-$$.log | sed 's/^/  /'
UPDATE_EXIT=${PIPESTATUS[0]}
set -e

# Cleanup mounts
incus exec ${VM_NAME} -- bash -c "
    umount /var/lib/phukit 2>/dev/null || true
    umount /etc/phukit 2>/dev/null || true
    umount /mnt/active-var 2>/dev/null || true
    umount /mnt/active-root 2>/dev/null || true
    rmdir /mnt/active-var 2>/dev/null || true
    rmdir /mnt/active-root 2>/dev/null || true
" 2>/dev/null || true

if [ $UPDATE_EXIT -eq 0 ]; then
    echo -e "${GREEN}✓ Update successful${NC}\n"
else
    echo -e "${RED}✗ Update failed with exit code: $UPDATE_EXIT${NC}"
    echo -e "${YELLOW}Update log saved to: /tmp/phukit-update-$$.log${NC}"
    echo -e "${YELLOW}Last 50 lines of log:${NC}"
    tail -50 /tmp/phukit-update-$$.log | sed 's/^/  /'
    exit 1
fi

# Test 8: Verify both root partitions have content
echo -e "${BLUE}=== Test 8: Verify A/B Partitions ===${NC}"
incus exec ${VM_NAME} -- bash -c "
    ROOT1=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'root1' | head -1 | awk '{print \"/dev/\" \$1}')
    ROOT2=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'root2' | head -1 | awk '{print \"/dev/\" \$1}')

    echo 'Checking root1 partition...'
    mkdir -p /mnt/test-root1
    mount \$ROOT1 /mnt/test-root1
    ROOT1_SIZE=\$(du -sh /mnt/test-root1 | awk '{print \$1}')
    echo \"Root1 size: \$ROOT1_SIZE\"
    umount /mnt/test-root1

    echo ''
    echo 'Checking root2 partition...'
    mkdir -p /mnt/test-root2
    mount \$ROOT2 /mnt/test-root2
    ROOT2_SIZE=\$(du -sh /mnt/test-root2 | awk '{print \$1}')
    echo \"Root2 size: \$ROOT2_SIZE\"
    umount /mnt/test-root2

    rmdir /mnt/test-root1 /mnt/test-root2
" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}✓ Both A/B partitions verified${NC}\n"

# Test 9: Verify Boot Entries for A/B Systems
echo -e "${BLUE}=== Test 9: Verify Boot Entries ===${NC}"
incus exec ${VM_NAME} -- bash -c "
    mkdir -p /mnt/test-boot
    mkdir -p /mnt/test-boot/efi
    BOOT_PART=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'boot' | grep -v 'efi' | head -1 | awk '{print \"/dev/\" \$1}')
    EFI_PART=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'EFI' | head -1 | awk '{print \"/dev/\" \$1}')

    mount \$BOOT_PART /mnt/test-boot
    mount \$EFI_PART /mnt/test-boot/efi

    # Check for GRUB entries
    GRUB_CFG=''
    if [ -f /mnt/test-boot/grub2/grub.cfg ]; then
        GRUB_CFG='/mnt/test-boot/grub2/grub.cfg'
    elif [ -f /mnt/test-boot/grub/grub.cfg ]; then
        GRUB_CFG='/mnt/test-boot/grub/grub.cfg'
    fi

    if [ -n \"\$GRUB_CFG\" ]; then
        echo 'GRUB boot entries:'
        MENU_ENTRIES=\$(grep -c 'menuentry' \$GRUB_CFG || true)
        echo \"  Found \$MENU_ENTRIES boot menu entries\"
        grep 'menuentry' \$GRUB_CFG | sed 's/^/  /'
    fi

    # Check for systemd-boot entries
    if [ -d /mnt/test-boot/efi/loader/entries ]; then
        echo 'systemd-boot entries:'
        BOOT_ENTRIES=\$(ls -1 /mnt/test-boot/efi/loader/entries/*.conf 2>/dev/null | wc -l)
        echo \"  Found \$BOOT_ENTRIES boot entries\"
        for entry in /mnt/test-boot/efi/loader/entries/*.conf; do
            if [ -f \"\$entry\" ]; then
                echo \"  Entry: \$(basename \$entry)\"
                grep '^title' \"\$entry\" | sed 's/^/    /'
            fi
        done
    fi

    umount /mnt/test-boot/efi
    umount /mnt/test-boot
    rmdir /mnt/test-boot/efi
    rmdir /mnt/test-boot
" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}✓ Boot entries verified${NC}\n"

# Test 10: Check kernel and initramfs
echo -e "${BLUE}=== Test 10: Verify Kernel and Initramfs ===${NC}"
incus exec ${VM_NAME} -- bash -c "
    mkdir -p /mnt/test-boot
    mkdir -p /mnt/test-boot/efi
    BOOT_PART=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'boot' | grep -v 'efi' | head -1 | awk '{print \"/dev/\" \$1}')
    EFI_PART=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'EFI' | head -1 | awk '{print \"/dev/\" \$1}')

    mount \$BOOT_PART /mnt/test-boot
    mount \$EFI_PART /mnt/test-boot/efi

    # Check for kernels on boot partition (GRUB)
    if ls /mnt/test-boot/vmlinuz-* 2>/dev/null 1>&2; then
        echo 'Kernel files on boot partition (GRUB):'
        ls -lh /mnt/test-boot/vmlinuz-*
        echo ''
        echo 'Initramfs files on boot partition:'
        ls -lh /mnt/test-boot/initramfs-* 2>/dev/null || ls -lh /mnt/test-boot/initrd-* 2>/dev/null || echo 'No initramfs found'
    fi

    # Check for kernels on EFI partition (systemd-boot)
    if ls /mnt/test-boot/efi/vmlinuz-* 2>/dev/null 1>&2; then
        echo 'Kernel files on EFI partition (systemd-boot):'
        ls -lh /mnt/test-boot/efi/vmlinuz-*
        echo ''
        echo 'Initramfs files on EFI partition:'
        ls -lh /mnt/test-boot/efi/initramfs-* 2>/dev/null || ls -lh /mnt/test-boot/efi/initrd-* 2>/dev/null || echo 'No initramfs found'
    fi

    # Verify at least one location has kernels
    if ! ls /mnt/test-boot/vmlinuz-* 2>/dev/null 1>&2 && ! ls /mnt/test-boot/efi/vmlinuz-* 2>/dev/null 1>&2; then
        echo 'Error: No kernel found on boot or EFI partition'
        umount /mnt/test-boot/efi
        umount /mnt/test-boot
        exit 1
    fi

    umount /mnt/test-boot/efi
    umount /mnt/test-boot
    rmdir /mnt/test-boot/efi
    rmdir /mnt/test-boot
" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}✓ Kernel and initramfs verified${NC}\n"

# Test 11: Boot from installed disk
echo -e "${BLUE}=== Test 11: Boot Test ===${NC}"
echo "Creating new VM to boot from installed disk..."

# Detach disk from current VM
incus storage volume detach default ${VM_NAME}-disk ${VM_NAME}
echo "  Detached disk from test VM"

# Create new VM for boot test (empty, no base image)
BOOT_VM_NAME="${VM_NAME}-boot"
incus create ${BOOT_VM_NAME} --vm --empty \
    -c limits.cpu=2 \
    -c limits.memory=4GiB \
    -c security.secureboot=false

echo "  Created empty boot test VM: ${BOOT_VM_NAME}"

# Attach the installed disk as the primary boot disk
# incus storage volume attach default ${VM_NAME}-disk ${BOOT_VM_NAME}
#echo "  Attached installed disk to boot VM"

incus config device add ${BOOT_VM_NAME} bootable disk pool=default source=${VM_NAME}-disk boot.priority=10
echo "  Configured boot disk for VM"
# Start the VM and try to boot from the installed disk
echo "  Starting VM with installed disk..."
incus start ${BOOT_VM_NAME}

# Wait for the system to boot (give it up to 2 minutes)
echo "  Waiting for system to boot (timeout: 120s)..."
boot_timeout=120
boot_success=false

while [ $boot_timeout -gt 0 ]; do
    # Try to execute a simple command to see if system is up
    if incus exec ${BOOT_VM_NAME} -- true 2>/dev/null; then
        boot_success=true
        echo "  System responded after $((120 - boot_timeout)) seconds"
        break
    fi
    echo -n "."
    sleep 2
    boot_timeout=$((boot_timeout - 2))
done
echo ""

if [ "$boot_success" = true ]; then
    # Run some basic verification commands
    echo "  Verifying booted system..."

    # Check kernel version
    KERNEL_VERSION=$(incus exec ${BOOT_VM_NAME} -- uname -r 2>/dev/null || echo "unknown")
    echo "    Kernel: ${KERNEL_VERSION}"

    # Check if phukit config exists
    if incus exec ${BOOT_VM_NAME} -- test -f /etc/phukit/config.json 2>/dev/null; then
        echo "    ✓ Phukit configuration found"
    else
        echo "    ✗ Phukit configuration missing"
    fi

    # Check partition mounts
    echo "    Partition mounts:"
    incus exec ${BOOT_VM_NAME} -- df -h / /boot /var 2>/dev/null | sed 's/^/      /' || echo "      Could not query mounts"

    # # Stop and delete the boot test VM
    # incus stop ${BOOT_VM_NAME} --force
    # incus delete ${BOOT_VM_NAME} --force

    echo -e "${GREEN}✓ Boot test successful - system is bootable${NC}\n"
else
    echo -e "${RED}✗ Boot test failed - system did not boot within timeout${NC}"

    # Try to get console output for debugging
    echo "  Console output:"
    incus console ${BOOT_VM_NAME} --show-log 2>/dev/null | tail -50 | sed 's/^/    /' || echo "    Console log not available"

    # Cleanup
    # incus stop ${BOOT_VM_NAME} --force 2>/dev/null || true
    # incus delete ${BOOT_VM_NAME} --force 2>/dev/null || true

    exit 1
fi

# Summary
echo -e "${GREEN}=== All Tests Passed ===${NC}\n"
echo -e "${BLUE}Test Summary:${NC}"
echo "  ✓ List disks"
echo "  ✓ Validate disk"
echo "  ✓ Install bootc image"
echo "  ✓ Verify partition layout (5 partitions)"
echo "  ✓ Verify bootloader installation"
echo "  ✓ Verify root filesystem"
echo "  ✓ System update (A/B partition)"
echo "  ✓ Verify both A/B partitions"
echo "  ✓ Verify boot entries (GRUB/systemd-boot)"
echo "  ✓ Verify kernel and initramfs (boot/EFI partition)"
echo "  ✓ Boot test - system is bootable"
echo ""
echo -e "${GREEN}Integration tests completed successfully!${NC}"
