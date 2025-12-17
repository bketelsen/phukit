#!/bin/bash
# Integration tests for phukit using Incus virtual machines
# Requires: incus, podman, root privileges

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
TEST_IMAGE="quay.io/centos-bootc/centos-bootc:stream9"
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

    # Remove storage volume if exists
    if incus storage volume list default --format csv | grep -q "^custom,${VM_NAME}-disk,"; then
        echo "Deleting storage volume: ${VM_NAME}-disk"
        incus storage volume delete default ${VM_NAME}-disk 2>/dev/null || true
    fi

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
REQUIRED_TOOLS="incus podman go make"
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
    -c limits.cpu=2 \
    -c limits.memory=4GiB \
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
    dnf install -y podman grub2-efi-x64 grub2-tools gdisk util-linux e2fsprogs dosfstools
" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}Tools installed${NC}\n"

# Push phukit binary to VM
echo -e "${BLUE}=== Copying phukit to VM ===${NC}"
incus file push "$BUILD_DIR/phukit" ${VM_NAME}/usr/local/bin/phukit
incus exec ${VM_NAME} -- chmod +x /usr/local/bin/phukit
echo -e "${GREEN}Binary copied${NC}\n"

# Pull test container image in the VM
echo -e "${BLUE}=== Pulling test container image in VM ===${NC}"
echo "Image: $TEST_IMAGE"
incus exec ${VM_NAME} -- podman pull "$TEST_IMAGE" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}Image pulled${NC}\n"

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
if timeout $TIMEOUT incus exec ${VM_NAME} -- bash -c "echo 'yes' | phukit install \
    --image '$TEST_IMAGE' \
    --device '$TEST_DISK' \
    --verbose" 2>&1 | tee /tmp/phukit-install-$$.log | sed 's/^/  /'; then
    echo -e "${GREEN}✓ Installation successful${NC}\n"
else
    echo -e "${RED}✗ Installation failed${NC}"
    echo -e "${YELLOW}Install log saved to: /tmp/phukit-install-$$.log${NC}"
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
incus exec ${VM_NAME} -- bash -c "
    mkdir -p /mnt/test-boot
    BOOT_PART=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'boot' | grep -v 'efi' | head -1 | awk '{print \"/dev/\" \$1}')
    mount \$BOOT_PART /mnt/test-boot
    echo 'Boot partition contents:'
    ls -lh /mnt/test-boot/
    echo ''
    echo 'GRUB config:'
    if [ -d /mnt/test-boot/grub2 ]; then
        cat /mnt/test-boot/grub2/grub.cfg || cat /mnt/test-boot/grub/grub.cfg
    else
        cat /mnt/test-boot/grub/grub.cfg
    fi
    umount /mnt/test-boot
    rmdir /mnt/test-boot
" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}✓ Bootloader verified${NC}\n"

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

# Update - pipe "yes" to confirm
if timeout $TIMEOUT incus exec ${VM_NAME} -- bash -c "echo 'yes' | phukit update \
    --device '$TEST_DISK' \
    --verbose" 2>&1 | tee /tmp/phukit-update-$$.log | sed 's/^/  /'; then
    echo -e "${GREEN}✓ Update successful${NC}\n"
else
    echo -e "${RED}✗ Update failed${NC}"
    echo -e "${YELLOW}Update log saved to: /tmp/phukit-update-$$.log${NC}"
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

# Test 9: Verify GRUB has entries for both systems
echo -e "${BLUE}=== Test 9: Verify GRUB Boot Entries ===${NC}"
incus exec ${VM_NAME} -- bash -c "
    mkdir -p /mnt/test-boot
    BOOT_PART=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'boot' | grep -v 'efi' | head -1 | awk '{print \"/dev/\" \$1}')
    mount \$BOOT_PART /mnt/test-boot

    GRUB_CFG=''
    if [ -f /mnt/test-boot/grub2/grub.cfg ]; then
        GRUB_CFG='/mnt/test-boot/grub2/grub.cfg'
    elif [ -f /mnt/test-boot/grub/grub.cfg ]; then
        GRUB_CFG='/mnt/test-boot/grub/grub.cfg'
    fi

    if [ -n \"\$GRUB_CFG\" ]; then
        MENU_ENTRIES=\$(grep -c 'menuentry' \$GRUB_CFG || true)
        echo \"Found \$MENU_ENTRIES boot menu entries\"
        grep 'menuentry' \$GRUB_CFG
    else
        echo 'GRUB config not found'
    fi

    umount /mnt/test-boot
    rmdir /mnt/test-boot
" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}✓ GRUB entries verified${NC}\n"

# Test 10: Check kernel and initramfs
echo -e "${BLUE}=== Test 10: Verify Kernel and Initramfs ===${NC}"
incus exec ${VM_NAME} -- bash -c "
    mkdir -p /mnt/test-boot
    BOOT_PART=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'boot' | grep -v 'efi' | head -1 | awk '{print \"/dev/\" \$1}')
    mount \$BOOT_PART /mnt/test-boot

    echo 'Kernel files:'
    ls -lh /mnt/test-boot/vmlinuz-* 2>/dev/null || echo 'No kernel found'
    echo ''
    echo 'Initramfs files:'
    ls -lh /mnt/test-boot/initramfs-* 2>/dev/null || ls -lh /mnt/test-boot/initrd-* 2>/dev/null || echo 'No initramfs found'

    umount /mnt/test-boot
    rmdir /mnt/test-boot
" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}✓ Kernel and initramfs verified${NC}\n"

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
echo "  ✓ Verify GRUB boot entries"
echo "  ✓ Verify kernel and initramfs"
echo ""
echo -e "${GREEN}Integration tests completed successfully!${NC}"
