#!/bin/bash
# Test script to demonstrate JSON output format
# This would normally be run during actual install/update operations

echo "=== Example of --output json format ==="
echo ""
echo "Command: phukit install --output json --image myimage:latest --device /dev/sda"
echo ""
echo "Output (JSON snapshots with complete state per message):"
echo ""

cat << 'EOF'
{"type":"phase_start","phase":"install","step":1,"total_steps":8,"current":"Validating image reference","status":"in_progress","progress":0,"timestamp":"2024-01-15T10:30:00Z"}
{"type":"log","phase":"install","step":1,"total_steps":8,"current":"Validating image reference","status":"in_progress","progress":0,"message":"Validating image reference: myimage:latest","logs":["Validating image reference: myimage:latest"],"timestamp":"2024-01-15T10:30:01Z"}
{"type":"log","phase":"install","step":1,"total_steps":8,"current":"Validating image reference","status":"in_progress","progress":0,"message":"  Image reference is valid and accessible","logs":["Validating image reference: myimage:latest","  Image reference is valid and accessible"],"timestamp":"2024-01-15T10:30:02Z"}
{"type":"phase_start","phase":"install","step":2,"total_steps":8,"current":"Preparing disk","status":"in_progress","progress":12,"timestamp":"2024-01-15T10:30:03Z"}
{"type":"log","phase":"install","step":2,"total_steps":8,"current":"Preparing disk","status":"in_progress","progress":12,"message":"Creating GPT partition table...","logs":["Creating GPT partition table..."],"timestamp":"2024-01-15T10:30:04Z"}
{"type":"phase_start","phase":"install","step":3,"total_steps":8,"current":"Creating partitions","status":"in_progress","progress":25,"timestamp":"2024-01-15T10:30:05Z"}
{"type":"phase_start","phase":"install","step":4,"total_steps":8,"current":"Creating filesystems","status":"in_progress","progress":37,"timestamp":"2024-01-15T10:30:06Z"}
{"type":"phase_start","phase":"install","step":5,"total_steps":8,"current":"Extracting container image","status":"in_progress","progress":50,"timestamp":"2024-01-15T10:30:07Z"}
{"type":"log","phase":"install","step":5,"total_steps":8,"current":"Extracting container image","status":"in_progress","progress":50,"message":"Extracting layer 1/3...","logs":["Extracting layer 1/3..."],"timestamp":"2024-01-15T10:30:08Z"}
{"type":"phase_start","phase":"install","step":6,"total_steps":8,"current":"Installing bootloader","status":"in_progress","progress":62,"timestamp":"2024-01-15T10:30:09Z"}
{"type":"log","phase":"install","step":6,"total_steps":8,"current":"Installing bootloader","status":"in_progress","progress":62,"message":"  Detected bootloader: systemd-boot","logs":["  Detected bootloader: systemd-boot"],"timestamp":"2024-01-15T10:30:10Z"}
{"type":"phase_start","phase":"install","step":7,"total_steps":8,"current":"Configuring system","status":"in_progress","progress":75,"timestamp":"2024-01-15T10:30:11Z"}
{"type":"phase_start","phase":"install","step":8,"total_steps":8,"current":"Finalizing installation","status":"in_progress","progress":87,"timestamp":"2024-01-15T10:30:12Z"}
{"type":"complete","phase":"install","status":"success","progress":100,"logs":[],"timestamp":"2024-01-15T10:30:13Z"}
EOF

echo ""
echo ""
echo "=== Example of --output text format (default) ==="
echo ""
echo "Command: phukit install --image myimage:latest --device /dev/sda"
echo ""
echo "Output:"
echo ""

cat << 'EOF'
Validating image reference: myimage:latest
  Image reference is valid and accessible

Step 1/8: Validating image reference...

Step 2/8: Preparing disk...
Creating GPT partition table...

Step 3/8: Creating partitions...

Step 4/8: Creating filesystems...

Step 5/8: Extracting container image...
Extracting layer 1/3...

Step 6/8: Installing bootloader...
  Detected bootloader: systemd-boot

Step 7/8: Configuring system...

Step 8/8: Finalizing installation...

============================================================
Operation completed successfully!
============================================================
EOF

echo ""
echo ""
echo "=== Key Features of JSON Output ==="
echo "1. Each line is a complete JSON object (JSON Snapshots)"
echo "2. Every event includes complete state (phase, step, progress, logs)"
echo "3. Logs accumulate throughout the phase"
echo "4. Progress is calculated as: (step - 1) * 100 / total_steps"
echo "5. Event types: phase_start, log, warning, error, complete"
echo "6. Status: in_progress, completed, failed, success"
echo "7. All events include ISO 8601 timestamp"
echo ""
echo "This format allows GUI installers to:"
echo "- Parse output line-by-line in real-time"
echo "- Display progress bars based on step/total_steps"
echo "- Show accumulated logs at any point"
echo "- Handle warnings and errors gracefully"
echo "- Track state transitions without parsing text"
