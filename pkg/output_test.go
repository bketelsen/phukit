package pkg

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNewOutputWriter(t *testing.T) {
	tests := []struct {
		name    string
		format  OutputFormat
		writer  *bytes.Buffer
		verbose bool
	}{
		{
			name:    "text format with writer",
			format:  OutputFormatText,
			writer:  &bytes.Buffer{},
			verbose: false,
		},
		{
			name:    "json format with writer",
			format:  OutputFormatJSON,
			writer:  &bytes.Buffer{},
			verbose: true,
		},
		{
			name:    "text format verbose",
			format:  OutputFormatText,
			writer:  &bytes.Buffer{},
			verbose: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ow := NewOutputWriter(tt.format, tt.writer, tt.verbose)
			if ow == nil {
				t.Fatal("NewOutputWriter returned nil")
			}
			if ow.format != tt.format {
				t.Errorf("format = %v, want %v", ow.format, tt.format)
			}
			if ow.verbose != tt.verbose {
				t.Errorf("verbose = %v, want %v", ow.verbose, tt.verbose)
			}
			if ow.logs == nil {
				t.Error("logs slice should be initialized")
			}
		})
	}
}

func TestNewOutputWriter_NilWriter(t *testing.T) {
	ow := NewOutputWriter(OutputFormatText, nil, false)
	if ow == nil {
		t.Fatal("NewOutputWriter returned nil")
	}
	// Should default to os.Stdout when writer is nil
	if ow.writer == nil {
		t.Error("writer should be set to os.Stdout when nil is passed")
	}
}

func TestSetPhase(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatText, buf, false)

	ow.SetPhase("install", 8)

	if ow.phase != "install" {
		t.Errorf("phase = %v, want %v", ow.phase, "install")
	}
	if ow.totalSteps != 8 {
		t.Errorf("totalSteps = %v, want %v", ow.totalSteps, 8)
	}
	if ow.step != 0 {
		t.Errorf("step = %v, want %v", ow.step, 0)
	}
}

func TestPhaseStart_TextFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatText, buf, false)
	ow.SetPhase("install", 8)

	ow.PhaseStart(1, "Validating image")

	output := buf.String()
	expected := "\nStep 1/8: Validating image...\n"
	if output != expected {
		t.Errorf("output = %q, want %q", output, expected)
	}

	if ow.step != 1 {
		t.Errorf("step = %v, want %v", ow.step, 1)
	}
	if ow.current != "Validating image" {
		t.Errorf("current = %v, want %v", ow.current, "Validating image")
	}
	if len(ow.logs) != 0 {
		t.Errorf("logs should be reset, got %d items", len(ow.logs))
	}
}

func TestPhaseStart_JSONFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatJSON, buf, false)
	ow.SetPhase("install", 8)

	ow.PhaseStart(1, "Validating image")

	var event OutputEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if event.Type != EventPhaseStart {
		t.Errorf("Type = %v, want %v", event.Type, EventPhaseStart)
	}
	if event.Phase != "install" {
		t.Errorf("Phase = %v, want %v", event.Phase, "install")
	}
	if event.Step != 1 {
		t.Errorf("Step = %v, want %v", event.Step, 1)
	}
	if event.TotalSteps != 8 {
		t.Errorf("TotalSteps = %v, want %v", event.TotalSteps, 8)
	}
	if event.Current != "Validating image" {
		t.Errorf("Current = %v, want %v", event.Current, "Validating image")
	}
	if event.Status != "in_progress" {
		t.Errorf("Status = %v, want %v", event.Status, "in_progress")
	}
	if event.Progress != 12 { // (1 * 100) / 8 = 12
		t.Errorf("Progress = %v, want %v", event.Progress, 12)
	}
	if event.Timestamp == "" {
		t.Error("Timestamp should not be empty")
	}
}

func TestPhaseComplete_JSONFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatJSON, buf, false)
	ow.SetPhase("install", 8)
	ow.PhaseStart(1, "Validating image")
	buf.Reset() // Clear previous output

	ow.PhaseComplete(1, "Validating image")

	var event OutputEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if event.Type != EventPhaseComplete {
		t.Errorf("Type = %v, want %v", event.Type, EventPhaseComplete)
	}
	if event.Status != "completed" {
		t.Errorf("Status = %v, want %v", event.Status, "completed")
	}
}

func TestPhaseComplete_TextFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatText, buf, false)
	ow.SetPhase("install", 8)

	ow.PhaseComplete(1, "Validating image")

	// Text format doesn't output anything for PhaseComplete
	if buf.Len() != 0 {
		t.Errorf("expected no output for text format PhaseComplete, got: %q", buf.String())
	}
}

func TestLog_TextFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatText, buf, false)
	ow.SetPhase("install", 8)

	ow.Log("Test message")

	output := strings.TrimSpace(buf.String())
	if output != "Test message" {
		t.Errorf("output = %q, want %q", output, "Test message")
	}

	if len(ow.logs) != 1 {
		t.Errorf("logs length = %d, want %d", len(ow.logs), 1)
	}
	if ow.logs[0] != "Test message" {
		t.Errorf("logs[0] = %q, want %q", ow.logs[0], "Test message")
	}
}

func TestLog_JSONFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatJSON, buf, false)
	ow.SetPhase("install", 8)
	ow.PhaseStart(1, "Validating image")
	buf.Reset()

	ow.Log("Test message")

	var event OutputEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if event.Type != EventLog {
		t.Errorf("Type = %v, want %v", event.Type, EventLog)
	}
	if event.Message != "Test message" {
		t.Errorf("Message = %v, want %v", event.Message, "Test message")
	}
	if len(event.Logs) != 1 {
		t.Errorf("Logs length = %d, want %d", len(event.Logs), 1)
	}
	if event.Status != "in_progress" {
		t.Errorf("Status = %v, want %v", event.Status, "in_progress")
	}
}

func TestLog_AccumulatesLogs(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatJSON, buf, false)
	ow.SetPhase("install", 8)
	ow.PhaseStart(1, "Validating image")

	ow.Log("First message")
	ow.Log("Second message")
	ow.Log("Third message")

	if len(ow.logs) != 3 {
		t.Errorf("logs length = %d, want %d", len(ow.logs), 3)
	}

	// Parse last JSON line to verify logs accumulated
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	lastLine := lines[len(lines)-1]

	var event OutputEvent
	if err := json.Unmarshal([]byte(lastLine), &event); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if len(event.Logs) != 3 {
		t.Errorf("event.Logs length = %d, want %d", len(event.Logs), 3)
	}
}

func TestLogf(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatText, buf, false)

	ow.Logf("Test %s with %d", "format", 123)

	output := strings.TrimSpace(buf.String())
	expected := "Test format with 123"
	if output != expected {
		t.Errorf("output = %q, want %q", output, expected)
	}
}

func TestWarning_TextFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatText, buf, false)

	ow.Warning("This is a warning")

	output := strings.TrimSpace(buf.String())
	expected := "Warning: This is a warning"
	if output != expected {
		t.Errorf("output = %q, want %q", output, expected)
	}

	if len(ow.logs) != 1 {
		t.Errorf("logs length = %d, want %d", len(ow.logs), 1)
	}
	if ow.logs[0] != "WARNING: This is a warning" {
		t.Errorf("logs[0] = %q, want %q", ow.logs[0], "WARNING: This is a warning")
	}
}

func TestWarning_JSONFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatJSON, buf, false)
	ow.SetPhase("install", 8)

	ow.Warning("This is a warning")

	var event OutputEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if event.Type != EventWarning {
		t.Errorf("Type = %v, want %v", event.Type, EventWarning)
	}
	if event.Message != "This is a warning" {
		t.Errorf("Message = %v, want %v", event.Message, "This is a warning")
	}
	if len(event.Logs) != 1 {
		t.Errorf("Logs length = %d, want %d", len(event.Logs), 1)
	}
	if !strings.Contains(event.Logs[0], "WARNING:") {
		t.Errorf("Logs[0] should contain WARNING prefix, got: %q", event.Logs[0])
	}
}

func TestError_TextFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatText, buf, false)

	err := errors.New("test error")
	ow.Error(err)

	output := strings.TrimSpace(buf.String())
	expected := "Error: test error"
	if output != expected {
		t.Errorf("output = %q, want %q", output, expected)
	}

	if len(ow.logs) != 1 {
		t.Errorf("logs length = %d, want %d", len(ow.logs), 1)
	}
	if ow.logs[0] != "ERROR: test error" {
		t.Errorf("logs[0] = %q, want %q", ow.logs[0], "ERROR: test error")
	}
}

func TestError_JSONFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatJSON, buf, false)
	ow.SetPhase("install", 8)

	err := errors.New("test error")
	ow.Error(err)

	var event OutputEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if event.Type != EventError {
		t.Errorf("Type = %v, want %v", event.Type, EventError)
	}
	if event.Error != "test error" {
		t.Errorf("Error = %v, want %v", event.Error, "test error")
	}
	if event.Status != "failed" {
		t.Errorf("Status = %v, want %v", event.Status, "failed")
	}
	if len(event.Logs) != 1 {
		t.Errorf("Logs length = %d, want %d", len(event.Logs), 1)
	}
}

func TestComplete_Success_TextFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatText, buf, false)

	ow.Complete(true, nil)

	output := buf.String()
	if !strings.Contains(output, "Operation completed successfully!") {
		t.Errorf("output should contain success message, got: %q", output)
	}
	if !strings.Contains(output, strings.Repeat("=", 60)) {
		t.Error("output should contain separator line")
	}
}

func TestComplete_Success_JSONFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatJSON, buf, false)
	ow.SetPhase("install", 8)

	ow.Complete(true, nil)

	var event OutputEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if event.Type != EventComplete {
		t.Errorf("Type = %v, want %v", event.Type, EventComplete)
	}
	if event.Status != "success" {
		t.Errorf("Status = %v, want %v", event.Status, "success")
	}
	if event.Error != "" {
		t.Errorf("Error should be empty, got: %q", event.Error)
	}
}

func TestComplete_Failure_TextFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatText, buf, false)

	err := errors.New("installation failed")
	ow.Complete(false, err)

	output := buf.String()
	if !strings.Contains(output, "Operation failed: installation failed") {
		t.Errorf("output should contain failure message, got: %q", output)
	}
}

func TestComplete_Failure_JSONFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatJSON, buf, false)
	ow.SetPhase("install", 8)

	err := errors.New("installation failed")
	ow.Complete(false, err)

	var event OutputEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if event.Type != EventComplete {
		t.Errorf("Type = %v, want %v", event.Type, EventComplete)
	}
	if event.Status != "failed" {
		t.Errorf("Status = %v, want %v", event.Status, "failed")
	}
	if event.Error != "installation failed" {
		t.Errorf("Error = %v, want %v", event.Error, "installation failed")
	}
}

func TestComplete_Failure_NoError(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatJSON, buf, false)
	ow.SetPhase("install", 8)

	ow.Complete(false, nil)

	var event OutputEvent
	if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if event.Status != "failed" {
		t.Errorf("Status = %v, want %v", event.Status, "failed")
	}
	if event.Error != "" {
		t.Errorf("Error should be empty when err is nil, got: %q", event.Error)
	}
}

func TestCalculateProgress(t *testing.T) {
	tests := []struct {
		name       string
		step       int
		totalSteps int
		want       int
	}{
		{"zero steps", 0, 0, 0},
		{"first step of 8", 1, 8, 12},
		{"second step of 8", 2, 8, 25},
		{"fourth step of 8", 4, 8, 50},
		{"last step of 8", 8, 8, 100},
		{"first step of 5", 1, 5, 20},
		{"third step of 5", 3, 5, 60},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ow := &OutputWriter{
				step:       tt.step,
				totalSteps: tt.totalSteps,
			}
			got := ow.calculateProgress()
			if got != tt.want {
				t.Errorf("calculateProgress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsJSON(t *testing.T) {
	tests := []struct {
		name   string
		format OutputFormat
		want   bool
	}{
		{"json format", OutputFormatJSON, true},
		{"text format", OutputFormatText, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ow := NewOutputWriter(tt.format, &bytes.Buffer{}, false)
			got := ow.IsJSON()
			if got != tt.want {
				t.Errorf("IsJSON() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsVerbose(t *testing.T) {
	tests := []struct {
		name    string
		verbose bool
		want    bool
	}{
		{"verbose enabled", true, true},
		{"verbose disabled", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ow := NewOutputWriter(OutputFormatText, &bytes.Buffer{}, tt.verbose)
			got := ow.IsVerbose()
			if got != tt.want {
				t.Errorf("IsVerbose() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOutputWriter_MultiplePhases(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatJSON, buf, false)

	// First phase
	ow.SetPhase("prepare", 2)
	ow.PhaseStart(1, "Checking prerequisites")
	ow.Log("Checking disk space")
	ow.PhaseStart(2, "Validating image")
	ow.Log("Image is valid")

	// Second phase
	ow.SetPhase("install", 3)
	ow.PhaseStart(1, "Creating partitions")
	ow.Log("Partitions created")

	// Verify logs were reset between phases
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 5 {
		t.Fatalf("expected at least 5 output lines, got %d", len(lines))
	}

	// Parse last event
	var lastEvent OutputEvent
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &lastEvent); err != nil {
		t.Fatalf("failed to unmarshal last JSON: %v", err)
	}

	// Should only have logs from current phase
	if lastEvent.Phase != "install" {
		t.Errorf("Phase = %v, want %v", lastEvent.Phase, "install")
	}
	if len(lastEvent.Logs) != 1 {
		t.Errorf("Logs from new phase should have 1 entry, got %d", len(lastEvent.Logs))
	}
}

func TestOutputWriter_JSONLineByLine(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatJSON, buf, false)
	ow.SetPhase("install", 3)

	ow.PhaseStart(1, "Step one")
	ow.Log("Message 1")
	ow.Log("Message 2")
	ow.PhaseStart(2, "Step two")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 JSON lines, got %d", len(lines))
	}

	// Each line should be valid JSON
	for i, line := range lines {
		var event OutputEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Errorf("line %d is not valid JSON: %v\nLine: %s", i, err, line)
		}
	}
}

func TestOutputWriter_ProgressIncrements(t *testing.T) {
	buf := &bytes.Buffer{}
	ow := NewOutputWriter(OutputFormatJSON, buf, false)
	ow.SetPhase("install", 4)

	steps := []struct {
		step     int
		name     string
		progress int
	}{
		{1, "Step 1", 25},
		{2, "Step 2", 50},
		{3, "Step 3", 75},
		{4, "Step 4", 100},
	}

	for _, s := range steps {
		buf.Reset()
		ow.PhaseStart(s.step, s.name)

		var event OutputEvent
		if err := json.Unmarshal(buf.Bytes(), &event); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if event.Progress != s.progress {
			t.Errorf("Step %d: Progress = %v, want %v", s.step, event.Progress, s.progress)
		}
	}
}
