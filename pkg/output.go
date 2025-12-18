package pkg

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// OutputFormat represents the output format type
type OutputFormat string

const (
	OutputFormatText OutputFormat = "text"
	OutputFormatJSON OutputFormat = "json"
)

// EventType represents the type of output event
type EventType string

const (
	EventPhaseStart    EventType = "phase_start"
	EventPhaseProgress EventType = "phase_progress"
	EventPhaseComplete EventType = "phase_complete"
	EventLog           EventType = "log"
	EventWarning       EventType = "warning"
	EventError         EventType = "error"
	EventComplete      EventType = "complete"
)

// OutputEvent represents a single output event in JSON format
type OutputEvent struct {
	Type       EventType `json:"type"`
	Phase      string    `json:"phase,omitempty"`
	Step       int       `json:"step,omitempty"`
	TotalSteps int       `json:"total_steps,omitempty"`
	Current    string    `json:"current,omitempty"`
	Status     string    `json:"status,omitempty"`
	Progress   int       `json:"progress,omitempty"`
	Message    string    `json:"message,omitempty"`
	Error      string    `json:"error,omitempty"`
	Timestamp  string    `json:"timestamp"`
	Logs       []string  `json:"logs,omitempty"`
}

// OutputWriter handles output in different formats
type OutputWriter struct {
	format     OutputFormat
	writer     io.Writer
	phase      string
	step       int
	totalSteps int
	current    string
	logs       []string
	verbose    bool
}

// NewOutputWriter creates a new OutputWriter
func NewOutputWriter(format OutputFormat, writer io.Writer, verbose bool) *OutputWriter {
	if writer == nil {
		writer = os.Stdout
	}
	return &OutputWriter{
		format:  format,
		writer:  writer,
		logs:    make([]string, 0),
		verbose: verbose,
	}
}

// SetPhase sets the current phase (install, update, etc.)
func (o *OutputWriter) SetPhase(phase string, totalSteps int) {
	o.phase = phase
	o.totalSteps = totalSteps
	o.step = 0
}

// PhaseStart indicates the start of a step
func (o *OutputWriter) PhaseStart(step int, name string) {
	o.step = step
	o.current = name
	o.logs = make([]string, 0) // Reset logs for new phase

	if o.format == OutputFormatJSON {
		o.emitJSON(OutputEvent{
			Type:       EventPhaseStart,
			Phase:      o.phase,
			Step:       step,
			TotalSteps: o.totalSteps,
			Current:    name,
			Status:     "in_progress",
			Progress:   o.calculateProgress(),
			Timestamp:  time.Now().Format(time.RFC3339),
		})
	} else {
		fmt.Fprintf(o.writer, "\nStep %d/%d: %s...\n", step, o.totalSteps, name)
	}
}

// PhaseComplete indicates completion of a step
func (o *OutputWriter) PhaseComplete(step int, name string) {
	if o.format == OutputFormatJSON {
		o.emitJSON(OutputEvent{
			Type:       EventPhaseComplete,
			Phase:      o.phase,
			Step:       step,
			TotalSteps: o.totalSteps,
			Current:    name,
			Status:     "completed",
			Progress:   o.calculateProgress(),
			Logs:       o.logs,
			Timestamp:  time.Now().Format(time.RFC3339),
		})
	}
	// Text format doesn't explicitly show completion (implied by next step)
}

// Log outputs a log message
func (o *OutputWriter) Log(message string) {
	o.logs = append(o.logs, message)

	if o.format == OutputFormatJSON {
		o.emitJSON(OutputEvent{
			Type:       EventLog,
			Phase:      o.phase,
			Step:       o.step,
			TotalSteps: o.totalSteps,
			Current:    o.current,
			Status:     "in_progress",
			Progress:   o.calculateProgress(),
			Message:    message,
			Logs:       o.logs,
			Timestamp:  time.Now().Format(time.RFC3339),
		})
	} else {
		fmt.Fprintln(o.writer, message)
	}
}

// Logf outputs a formatted log message
func (o *OutputWriter) Logf(format string, args ...interface{}) {
	o.Log(fmt.Sprintf(format, args...))
}

// Warning outputs a warning message
func (o *OutputWriter) Warning(message string) {
	o.logs = append(o.logs, "WARNING: "+message)

	if o.format == OutputFormatJSON {
		o.emitJSON(OutputEvent{
			Type:       EventWarning,
			Phase:      o.phase,
			Step:       o.step,
			TotalSteps: o.totalSteps,
			Current:    o.current,
			Status:     "in_progress",
			Progress:   o.calculateProgress(),
			Message:    message,
			Logs:       o.logs,
			Timestamp:  time.Now().Format(time.RFC3339),
		})
	} else {
		fmt.Fprintf(o.writer, "Warning: %s\n", message)
	}
}

// Error outputs an error message
func (o *OutputWriter) Error(err error) {
	message := err.Error()
	o.logs = append(o.logs, "ERROR: "+message)

	if o.format == OutputFormatJSON {
		o.emitJSON(OutputEvent{
			Type:       EventError,
			Phase:      o.phase,
			Step:       o.step,
			TotalSteps: o.totalSteps,
			Current:    o.current,
			Status:     "failed",
			Progress:   o.calculateProgress(),
			Error:      message,
			Logs:       o.logs,
			Timestamp:  time.Now().Format(time.RFC3339),
		})
	} else {
		fmt.Fprintf(o.writer, "Error: %s\n", message)
	}
}

// Complete indicates overall operation completion
func (o *OutputWriter) Complete(success bool, err error) {
	status := "success"
	var errorMsg string
	if !success {
		status = "failed"
		if err != nil {
			errorMsg = err.Error()
		}
	}

	if o.format == OutputFormatJSON {
		o.emitJSON(OutputEvent{
			Type:      EventComplete,
			Phase:     o.phase,
			Status:    status,
			Progress:  o.calculateProgress(),
			Error:     errorMsg,
			Logs:      o.logs,
			Timestamp: time.Now().Format(time.RFC3339),
		})
	} else {
		if success {
			fmt.Fprintln(o.writer, "\n"+strings.Repeat("=", 60))
			fmt.Fprintf(o.writer, "Operation completed successfully!\n")
			fmt.Fprintln(o.writer, strings.Repeat("=", 60))
		} else {
			fmt.Fprintf(o.writer, "\nOperation failed: %s\n", errorMsg)
		}
	}
}

// calculateProgress calculates the current progress percentage
func (o *OutputWriter) calculateProgress() int {
	if o.totalSteps == 0 {
		return 0
	}
	return (o.step * 100) / o.totalSteps
}

// emitJSON emits a JSON event
func (o *OutputWriter) emitJSON(event OutputEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		// Fallback to text output if JSON marshaling fails
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		return
	}
	fmt.Fprintln(o.writer, string(data))
}

// IsJSON returns true if the output format is JSON
func (o *OutputWriter) IsJSON() bool {
	return o.format == OutputFormatJSON
}

// IsVerbose returns true if verbose output is enabled
func (o *OutputWriter) IsVerbose() bool {
	return o.verbose
}
