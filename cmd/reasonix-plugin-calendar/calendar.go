package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
)

// calendarEvent represents a single calendar event returned by read_event.
type calendarEvent struct {
	Title       string `json:"title"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Location    string `json:"location,omitempty"`
	Description string `json:"description,omitempty"`
	Source       string `json:"source"`
}

// runReadEvent reads calendar events from the system calendar within the
// specified date range. It dispatches to the platform-specific implementation.
func runReadEvent(args map[string]any) (any, error) {
	startDate, err := argString(args, "start_date")
	if err != nil {
		return nil, err
	}
	endDate, err := argString(args, "end_date")
	if err != nil {
		return nil, err
	}

	var events []calendarEvent
	switch runtime.GOOS {
	case "windows":
		events, err = readEventsWindows(startDate, endDate)
	case "darwin":
		events, err = readEventsMacOS(startDate, endDate)
	default:
		return fmt.Sprintf("Calendar reading is not supported on %s. Supported platforms: Windows (Outlook), macOS (Calendar.app).", runtime.GOOS), nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to read calendar events: %w", err)
	}

	if len(events) == 0 {
		return fmt.Sprintf("No calendar events found between %s and %s.", startDate, endDate), nil
	}

	b, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal events: %w", err)
	}
	return string(b), nil
}

// runAddEvent creates a new calendar event in the system calendar.
func runAddEvent(args map[string]any) (any, error) {
	title, err := argString(args, "title")
	if err != nil {
		return nil, err
	}
	startTime, err := argString(args, "start_time")
	if err != nil {
		return nil, err
	}
	endTime, err := argString(args, "end_time")
	if err != nil {
		return nil, err
	}
	description := argStringDefault(args, "description", "")
	location := argStringDefault(args, "location", "")

	switch runtime.GOOS {
	case "windows":
		return addEventWindows(title, startTime, endTime, description, location)
	case "darwin":
		return addEventMacOS(title, startTime, endTime, description, location)
	default:
		return fmt.Sprintf("Calendar event creation is not supported on %s. Supported platforms: Windows (Outlook), macOS (Calendar.app).", runtime.GOOS), nil
	}
}

// --- Windows implementation (Outlook via PowerShell COM) ---

func readEventsWindows(startDate, endDate string) ([]calendarEvent, error) {
	// Use PowerShell to read Outlook calendar via COM automation.
	// The script outputs JSON lines for each event found.
	psScript := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
try {
    $outlook = New-Object -ComObject Outlook.Application
    $namespace = $outlook.GetNamespace('MAPI')
    $calendar = $namespace.GetDefaultFolder(9)  # olFolderCalendar
    $items = $calendar.Items
    $items.Sort('[Start]')
    $items.IncludeRecurrences = $true
    $filter = "[Start] >= '%s' AND [End] <= '%s'"
    $filtered = $items.Restrict($filter)
    $results = @()
    foreach ($item in $filtered) {
        $results += @{
            title       = $item.Subject
            start       = $item.Start.ToString('yyyy-MM-ddTHH:mm:ss')
            end         = $item.End.ToString('yyyy-MM-ddTHH:mm:ss')
            location    = if ($item.Location) { $item.Location } else { '' }
            description = if ($item.Body) { $item.Body.Substring(0, [Math]::Min(500, $item.Body.Length)) } else { '' }
            source      = 'Outlook'
        }
    }
    $results | ConvertTo-Json -Compress
} catch {
    Write-Error $_.Exception.Message
}`, startDate, endDate)

	out, err := execPowerShell(psScript)
	if err != nil {
		log.Printf("PowerShell Outlook read failed: %v", err)
		return nil, fmt.Errorf("Outlook calendar access failed: %w (ensure Outlook is installed and configured)", err)
	}

	out = strings.TrimSpace(out)
	if out == "" || out == "null" {
		return nil, nil
	}

	var events []calendarEvent
	if err := json.Unmarshal([]byte(out), &events); err != nil {
		// Single event comes back as an object, not an array.
		var single calendarEvent
		if err2 := json.Unmarshal([]byte(out), &single); err2 != nil {
			return nil, fmt.Errorf("parse Outlook response: %w", err)
		}
		events = []calendarEvent{single}
	}
	return events, nil
}

func addEventWindows(title, startTime, endTime, description, location string) (any, error) {
	psScript := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
try {
    $outlook = New-Object -ComObject Outlook.Application
    $item = $outlook.CreateItem(1)  # olAppointmentItem
    $item.Subject = '%s'
    $item.Start = [DateTime]::Parse('%s')
    $item.End = [DateTime]::Parse('%s')
    %s
    %s
    $item.Save()
    Write-Output 'OK'
} catch {
    Write-Error $_.Exception.Message
}`,
		escapePS(title),
		escapePS(startTime),
		escapePS(endTime),
		psOptionalField("Body", description),
		psOptionalField("Location", location),
	)

	out, err := execPowerShell(psScript)
	if err != nil {
		return nil, fmt.Errorf("failed to create Outlook event: %w", err)
	}
	if strings.TrimSpace(out) == "OK" {
		return fmt.Sprintf("Event %q created in Outlook calendar (%s - %s).", title, startTime, endTime), nil
	}
	return fmt.Sprintf("Event creation returned: %s", strings.TrimSpace(out)), nil
}

// --- macOS implementation (icalBuddy / AppleScript) ---

func readEventsMacOS(startDate, endDate string) ([]calendarEvent, error) {
	// Try icalBuddy first (commonly available via Homebrew).
	if _, err := exec.LookPath("icalBuddy"); err == nil {
		return readEventsMacOSIcalBuddy(startDate, endDate)
	}
	// Fall back to AppleScript.
	return readEventsMacOSAppleScript(startDate, endDate)
}

func readEventsMacOSIcalBuddy(startDate, endDate string) ([]calendarEvent, error) {
	// icalBuddy eventsFrom:startDAte to:endDate
	args := []string{
		"-b", "",   // no bullet prefix
		"-n", "",   // no relative date
		"-li", "0", // no limit
		"-ea",      // include end dates
		"eventsFrom:" + startDate, "to:" + endDate,
	}
	out, err := exec.Command("icalBuddy", args...).Output()
	if err != nil {
		log.Printf("icalBuddy failed: %v", err)
		return nil, fmt.Errorf("icalBuddy execution failed: %w", err)
	}

	// Parse icalBuddy's text output into structured events.
	return parseIcalBuddyOutput(string(out)), nil
}

func readEventsMacOSAppleScript(startDate, endDate string) ([]calendarEvent, error) {
	script := fmt.Sprintf(`
set startDate to date "%s"
set endDate to date "%s"
set output to ""
tell application "Calendar"
    repeat with cal in calendars
        set theEvents to (every event of cal whose start date >= startDate and start date <= endDate)
        repeat with evt in theEvents
            set evtTitle to summary of evt
            set evtStart to start date of evt as string
            set evtEnd to end date of evt as string
            set evtLocation to ""
            try
                set evtLocation to location of evt
            end try
            set evtDesc to ""
            try
                set evtDesc to description of evt
            end try
            set output to output & evtTitle & "|" & evtStart & "|" & evtEnd & "|" & evtLocation & "|" & evtDesc & linefeed
        end repeat
    end repeat
end tell
return output`, startDate, endDate)

	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		log.Printf("AppleScript Calendar read failed: %v", err)
		return nil, fmt.Errorf("Calendar.app access failed: %w (ensure Calendar permissions are granted)", err)
	}

	return parseAppleScriptOutput(string(out)), nil
}

func addEventMacOS(title, startTime, endTime, description, location string) (any, error) {
	descPart := ""
	if description != "" {
		descPart = fmt.Sprintf(`set description of newEvent to "%s"`, escapeAS(description))
	}
	locPart := ""
	if location != "" {
		locPart = fmt.Sprintf(`set location of newEvent to "%s"`, escapeAS(location))
	}

	script := fmt.Sprintf(`
tell application "Calendar"
    tell calendar "Calendar"
        set startDate to date "%s"
        set endDate to date "%s"
        set newEvent to make new event with properties {summary:"%s", start date:startDate, end date:endDate}
        %s
        %s
    end tell
end tell`, escapeAS(startTime), escapeAS(endTime), escapeAS(title), descPart, locPart)

	_, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return nil, fmt.Errorf("failed to create Calendar.app event: %w", err)
	}
	return fmt.Sprintf("Event %q created in Calendar.app (%s - %s).", title, startTime, endTime), nil
}

// --- output parsers ---

// parseIcalBuddyOutput converts icalBuddy's text format into calendarEvent
// structs. icalBuddy outputs events in a loosely structured format with
// title, date/time, and location lines.
func parseIcalBuddyOutput(text string) []calendarEvent {
	var events []calendarEvent
	lines := strings.Split(text, "\n")
	var current *calendarEvent
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if current != nil && current.Title != "" {
				events = append(events, *current)
				current = nil
			}
			continue
		}
		if current == nil {
			current = &calendarEvent{Source: "Calendar.app (icalBuddy)"}
		}
		// Heuristic: first non-empty line is the title, subsequent lines
		// contain date/time and location info.
		if current.Title == "" {
			current.Title = line
		} else if strings.Contains(line, "at") || strings.Contains(line, "–") || strings.Contains(line, "-") {
			// Looks like a date/time line.
			current.Start = line
		} else if strings.Contains(line, "location") || strings.Contains(line, "Location") {
			current.Location = strings.TrimPrefix(line, "location:")
			current.Location = strings.TrimPrefix(current.Location, "Location:")
			current.Location = strings.TrimSpace(current.Location)
		}
	}
	if current != nil && current.Title != "" {
		events = append(events, *current)
	}
	return events
}

// parseAppleScriptOutput converts the pipe-delimited AppleScript output into
// calendarEvent structs.
func parseAppleScriptOutput(text string) []calendarEvent {
	var events []calendarEvent
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 5)
		if len(parts) < 3 {
			continue
		}
		evt := calendarEvent{
			Title:    parts[0],
			Start:    parts[1],
			End:      parts[2],
			Source:   "Calendar.app",
		}
		if len(parts) > 3 {
			evt.Location = parts[3]
		}
		if len(parts) > 4 {
			evt.Description = parts[4]
		}
		events = append(events, evt)
	}
	return events
}

// --- helpers ---

// execPowerShell runs a PowerShell script and returns its stdout.
func execPowerShell(script string) (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, string(out))
	}
	return string(out), nil
}

// escapePS escapes a string for embedding in a PowerShell single-quoted string.
func escapePS(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// escapeAS escapes a string for embedding in an AppleScript double-quoted string.
func escapeAS(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// psOptionalField returns a PowerShell assignment line if value is non-empty.
func psOptionalField(field, value string) string {
	if value == "" {
		return ""
	}
	return fmt.Sprintf("$item.%s = '%s'", field, escapePS(value))
}
