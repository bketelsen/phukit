# Machine-Readable Output Implementation

## Overview

Added `--output` flag to `phukit install` and `phukit update` commands to support machine-readable JSON output format for GUI installers and automation tools.

## Usage

```bash
# Text output (default, human-readable)
phukit install --image myimage:latest --device /dev/sda

# JSON output (machine-readable)
phukit install --output json --image myimage:latest --device /dev/sda
phukit update --output json --image myimage:v2.0
```

## JSON Output Format

### Design: JSON Snapshots (Option 4)

Each output line is a complete, self-contained JSON object representing the current state. This allows consumers to:

- Parse line-by-line in real-time
- Get complete state from any single message
- Display progress without maintaining complex state

### Event Types

1. **phase_start** - Beginning of a new step
2. **log** - Progress log message
3. **warning** - Warning message
4. **error** - Error message
5. **complete** - Overall operation completion

### JSON Schema

```json
{
  "type": "phase_start|log|warning|error|complete",
  "phase": "install|update",
  "step": 1,
  "total_steps": 8,
  "current": "Step description",
  "status": "in_progress|completed|failed|success",
  "progress": 12,
  "message": "Log message",
  "error": "Error message if failed",
  "timestamp": "2024-01-15T10:30:00Z",
  "logs": ["Accumulated log messages"]
}
```

### Key Features

- **Complete State**: Every JSON event includes phase, step, progress, and accumulated logs
- **Progress Tracking**: `progress` field is calculated as `(step - 1) * 100 / total_steps`
- **Log Accumulation**: `logs` array contains all log messages for current phase
- **Timestamps**: ISO 8601 format timestamps on every event
- **Streaming**: One JSON object per line for easy streaming parsing

## Implementation Details

### Files Modified

1. **pkg/output.go** (NEW)

   - `OutputWriter` type with support for text and JSON formats
   - `OutputFormat` type (text or json)
   - Event emission methods: `PhaseStart()`, `Log()`, `Logf()`, `Warning()`, `Error()`, `Complete()`
   - JSON snapshot emission with complete state

2. **pkg/bootc.go**

   - Added `Output *OutputWriter` field to `BootcInstaller`
   - Converted all `fmt.Print*` calls to use `Output.Log()` or `Output.Logf()`
   - Phase tracking with `SetPhase()` and `PhaseStart()`

3. **pkg/update.go**

   - Added `Output *OutputWriter` field to `SystemUpdater`
   - Added `SetOutput()` method
   - Converted all methods to use OutputWriter:
     - `PrepareUpdate()`
     - `PullImage()`
     - `Update()`
     - `InstallKernelAndInitramfs()`
     - `UpdateBootloader()`
     - `updateGRUBBootloader()`
     - `updateSystemdBootBootloader()`
     - `PerformUpdate()`

4. **cmd/root.go**

   - Added `--output` persistent flag (text or json)

5. **cmd/install.go**

   - Creates `OutputWriter` based on `--output` flag
   - Passes OutputWriter to installer

6. **cmd/update.go**
   - Creates `OutputWriter` based on `--output` flag
   - Passes OutputWriter to updater via `SetOutput()`

### API Methods

```go
// Create output writer
output := pkg.NewOutputWriter(pkg.OutputFormatJSON, os.Stdout, verbose)

// Set phase and total steps
output.SetPhase("install", 8)

// Start a step
output.PhaseStart(1, "Validating image reference")

// Log messages
output.Log("Message")
output.Logf("Formatted %s", "message")

// Warnings and errors
output.Warning("Warning message")
output.Error(err)

// Complete operation
output.Complete(true, nil)  // success
output.Complete(false, err) // failure
```

## Example Output

### Text Format (Default)

```
Step 1/8: Validating image reference...
Validating image reference: myimage:latest
  Image reference is valid and accessible

Step 2/8: Preparing disk...
Creating GPT partition table...

...

============================================================
Operation completed successfully!
============================================================
```

### JSON Format

```json
{"type":"phase_start","phase":"install","step":1,"total_steps":8,"current":"Validating image reference","status":"in_progress","progress":0,"timestamp":"2024-01-15T10:30:00Z"}
{"type":"log","phase":"install","step":1,"total_steps":8,"current":"Validating image reference","status":"in_progress","progress":0,"message":"Validating image reference: myimage:latest","logs":["Validating image reference: myimage:latest"],"timestamp":"2024-01-15T10:30:01Z"}
{"type":"log","phase":"install","step":1,"total_steps":8,"current":"Validating image reference","status":"in_progress","progress":0,"message":"  Image reference is valid and accessible","logs":["Validating image reference: myimage:latest","  Image reference is valid and accessible"],"timestamp":"2024-01-15T10:30:02Z"}
{"type":"phase_start","phase":"install","step":2,"total_steps":8,"current":"Preparing disk","status":"in_progress","progress":12,"timestamp":"2024-01-15T10:30:03Z"}
...
{"type":"complete","phase":"install","status":"success","progress":100,"logs":[],"timestamp":"2024-01-15T10:30:13Z"}
```

## Consumer Implementation Guide

### Parsing JSON Output

```python
import json
import subprocess

# Run phukit with JSON output
proc = subprocess.Popen(
    ["phukit", "install", "--output", "json", "--image", "myimage:latest", "--device", "/dev/sda"],
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    text=True
)

# Parse output line by line
for line in proc.stdout:
    event = json.loads(line)

    # Update progress bar
    if "progress" in event:
        update_progress_bar(event["progress"])

    # Display current step
    if event["type"] == "phase_start":
        print(f"Step {event['step']}/{event['total_steps']}: {event['current']}")

    # Display log messages
    elif event["type"] == "log":
        print(f"  {event['message']}")

    # Handle warnings
    elif event["type"] == "warning":
        print(f"⚠ {event['message']}")

    # Handle errors
    elif event["type"] == "error":
        print(f"❌ {event['error']}")

    # Check completion
    elif event["type"] == "complete":
        if event["status"] == "success":
            print("✅ Installation complete!")
        else:
            print(f"❌ Installation failed: {event['error']}")
```

## Benefits for GUI Installers

1. **Real-time Progress**: Get step number and progress percentage for progress bars
2. **State Tracking**: Each event contains complete state - no need to track state client-side
3. **Log Accumulation**: `logs` array provides complete history at any point
4. **Error Handling**: Structured error messages with context
5. **Streaming Friendly**: Parse line-by-line without buffering entire output
6. **Machine Parseable**: No regex needed - just JSON.parse() each line
7. **Timestamps**: Track duration and timing of operations

## Testing

Run the demonstration script:

```bash
./test_json_output.sh
```

This shows example output for both text and JSON formats.

## Future Enhancements

Potential additions for other commands:

1. **list command**: Simple JSON array of partitions
2. **validate command**: JSON object with validation results
3. **Progress details**: Add bytes transferred for large operations
4. **Nested phases**: Support sub-steps within steps
5. **Status endpoint**: Separate command to query current installation status

## Compatibility

- JSON output goes to stdout only
- Interactive prompts (like confirmation) still use stdout/stdin for user interaction
- Errors returned as exit codes remain unchanged
- Text format is default - fully backward compatible
