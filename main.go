// ics2csv is a small tool that convert
// an iCalendar (.ics) file to a CSV file.
package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/arran4/golang-ical"
	"github.com/spf13/pflag"
	rrule "github.com/teambition/rrule-go"
)

var version = "dev" // set by build system

// Parameters holds command-line parameters.
type Parameters struct {
	InputFile  string // path to input ICS file
	OutputFile string // path to output CSV file, or "stdout"
	Multiline  bool   // whether to preserve newlines in fields
}

// ParseFlags parses command-line flags and returns Parameters.
func ParseFlags() Parameters {
	var (
		icsFile   string
		csvFile   string
		multiline bool
	)

	// Register supported flags and command-specific usage text.
	pflag.StringVarP(&csvFile, "output", "o", "", "Output CSV file or 'stdout'(default: input name with .csv extension)")
	pflag.BoolVarP(&multiline, "multiline", "m", false, "Preserve newlines and whitespace in fields")
	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "ics2csv (version %s): convert ICS to CSV\n", version)
		fmt.Fprintf(os.Stderr, "Usage: ics2csv <input.ics> [--output <output.csv>|stdout]\n")
		pflag.PrintDefaults()
	}

	// Parse arguments, require an input, and derive the default output path.
	pflag.Parse()
	if pflag.NArg() < 1 {
		pflag.Usage()
		os.Exit(1)
	}
	icsFile = pflag.Arg(0)
	if csvFile == "" {
		csvFile = strings.TrimSuffix(icsFile, ".ics") + ".csv"
	}
	return Parameters{
		InputFile:  icsFile,
		OutputFile: csvFile,
		Multiline:  multiline,
	}
}

// Event represents a calendar event.
// The fields correspond to CSV columns.
type Event struct {
	Subject     string
	StartDate   string
	StartTime   string
	EndDate     string
	EndTime     string
	Description string
	Location    string
}

// ToSlice converts the Event to a slice of strings for CSV writing.
func (e *Event) ToSlice() []string {
	return []string{
		e.Subject,
		e.StartDate,
		e.StartTime,
		e.EndDate,
		e.EndTime,
		e.Description,
		e.Location,
	}
}

// Header returns the CSV header row.
func Header() []string {
	return []string{
		"Subject",
		"Start Date",
		"Start Time",
		"End Date",
		"End Time",
		"Description",
		"Location",
	}
}

// normalizeText preserves line breaks when requested and otherwise collapses whitespace.
func normalizeText(value string, preserveMultiline bool) string {
	if preserveMultiline {
		return value
	}
	return strings.Join(strings.Fields(value), " ")
}

// parseDateTimeToTime parses raw iCal datetime string into time.Time using optional TZID.
func parseDateTimeToTime(raw string, tz string) (time.Time, error) {
	layouts := []string{"20060102T150405Z07:00", "20060102T1504Z07:00", "20060102T150405", "20060102T1504", "20060102"}
	var parsed time.Time
	var err error
	// Try parsing in the supplied timezone before treating the value as floating or UTC.
	if tz != "" {
		if loc, lerr := time.LoadLocation(tz); lerr == nil {
			for _, L := range layouts {
				parsed, err = time.ParseInLocation(L, raw, loc)
				if err == nil {
					return parsed, nil
				}
			}
		}
	}
	// Try the same supported layouts without a named timezone.
	for _, L := range layouts {
		parsed, err = time.Parse(L, raw)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse datetime: %s", raw)
}

// getTZID returns the first TZID parameter value with surrounding quotes removed.
func getTZID(parameters map[string][]string) string {
	values := parameters["TZID"]
	if len(values) == 0 {
		return ""
	}
	return strings.Trim(values[0], "\"")
}

// getDateTime resolves a date-time through the library, then falls back to its raw property.
func getDateTime(vevent *ics.VEvent, propertyName string, getter func() (time.Time, error)) (time.Time, error) {
	// Prefer the calendar library's parsed value.
	value, err := getter()
	if err == nil {
		return value, nil
	}

	// Retry with the raw property when the library cannot parse it.
	if p := vevent.GetProperty(ics.ComponentProperty(propertyName)); p != nil {
		if value, parseErr := parseDateTimeToTime(p.Value, getTZID(p.ICalParameters)); parseErr == nil {
			return value, nil
		}
	}
	return time.Time{}, err
}

// formatDateTime returns date and time strings, or blanks when parsing failed.
func formatDateTime(value time.Time, err error) (string, string) {
	if err != nil {
		return "", ""
	}
	return value.Format("2006-01-02"), value.Format("15:04:05")
}

// expandRecurrences returns a list of start times for the event, expanding RRULE/RDATE and applying EXDATE.
// If RRULE has no UNTIL/COUNT, expansion is limited to one year from DTSTART.
func expandRecurrences(vevent *ics.VEvent, dtstart time.Time) ([]time.Time, error) {
	var occs []time.Time
	// Collect exclusions before generating recurrence instances.
	exmap := map[int64]bool{}
	for _, p := range vevent.GetProperties(ics.ComponentPropertyExdate) {
		tz := getTZID(p.ICalParameters)
		for _, raw := range strings.Split(p.Value, ",") {
			if excluded, err := parseDateTimeToTime(raw, tz); err == nil {
				exmap[excluded.UnixNano()] = true
			}
		}
	}

	// Expand RRULE within a one-year window; explicit COUNT or UNTIL still applies.
	rruleProp := vevent.GetProperty(ics.ComponentPropertyRrule)
	if rruleProp != nil {
		ruleStr := rruleProp.Value
		r, err := rrule.StrToRRule(ruleStr)
		if err != nil {
			return nil, err
		}
		// Anchor generated instances to the event's DTSTART.
		if setter := r.DTStart; setter != nil {
			setter(dtstart)
		}
		windowEnd := dtstart.AddDate(1, 0, 0)
		occs = r.Between(dtstart, windowEnd, true)
	}
	// Retain DTSTART if expansion produced no instances.
	if len(occs) == 0 {
		occs = []time.Time{dtstart}
	}
	// Remove instances listed by EXDATE.
	var filtered []time.Time
	for _, o := range occs {
		if !exmap[o.UnixNano()] {
			filtered = append(filtered, o)
		}
	}
	return filtered, nil
}

// propertyText retrieves and normalizes an optional text property.
func propertyText(vevent *ics.VEvent, property ics.ComponentProperty, preserveMultiline bool) string {
	p := vevent.GetProperty(property)
	if p == nil {
		return ""
	}
	return normalizeText(p.Value, preserveMultiline)
}

// eventsFromVEvent converts one calendar event into zero or more CSV rows.
func eventsFromVEvent(vevent *ics.VEvent, preserveMultiline bool) []Event {
	// A summary is required because it becomes the CSV subject.
	summary := vevent.GetProperty(ics.ComponentPropertySummary)
	if summary == nil {
		return nil
	}

	event := Event{
		Subject:     normalizeText(summary.Value, preserveMultiline),
		Description: propertyText(vevent, ics.ComponentPropertyDescription, preserveMultiline),
		Location:    propertyText(vevent, ics.ComponentPropertyLocation, preserveMultiline),
	}
	start, startErr := getDateTime(vevent, "DTSTART", vevent.GetStartAt)
	end, endErr := getDateTime(vevent, "DTEND", vevent.GetEndAt)

	// Recurring events produce one CSV row for each generated instance.
	if vevent.HasProperty(ics.ComponentPropertyRrule) && startErr == nil {
		duration := time.Duration(0)
		if endErr == nil {
			duration = end.Sub(start)
		}

		occurrences, err := expandRecurrences(vevent, start)
		if err != nil {
			occurrences = []time.Time{start}
		}

		events := make([]Event, 0, len(occurrences))
		for _, occurrence := range occurrences {
			recurringEvent := event
			recurringEvent.StartDate, recurringEvent.StartTime = formatDateTime(occurrence, nil)
			if endErr == nil {
				occurrenceEnd := end
				if duration != 0 {
					occurrenceEnd = occurrence.Add(duration)
				}
				recurringEvent.EndDate, recurringEvent.EndTime = formatDateTime(occurrenceEnd, nil)
			}
			events = append(events, recurringEvent)
		}
		return events
	}

	// Non-recurring events produce at most one row; invalid dates remain blank.
	event.StartDate, event.StartTime = formatDateTime(start, startErr)
	event.EndDate, event.EndTime = formatDateTime(end, endErr)
	if event.Subject == "" {
		return nil
	}
	return []Event{event}
}

// run parses the input calendar and writes its converted events to CSV.
func run() error {
	// Parse the input calendar before opening the output destination.
	params := ParseFlags()
	icsFile, err := os.Open(params.InputFile)
	if err != nil {
		return fmt.Errorf("open ICS file: %w", err)
	}
	defer icsFile.Close()

	calendar, err := ics.ParseCalendar(icsFile)
	if err != nil {
		return fmt.Errorf("parse ICS file: %w", err)
	}

	// Select stdout or create the requested CSV file.
	var csvFile *os.File
	if params.OutputFile == "stdout" {
		csvFile = os.Stdout
	} else {
		csvFile, err = os.Create(params.OutputFile)
		if err != nil {
			return fmt.Errorf("create CSV file: %w", err)
		}
		defer csvFile.Close()
	}

	// Write the header and converted rows, then check deferred CSV errors.
	csvWriter := csv.NewWriter(csvFile)
	if err := csvWriter.Write(Header()); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	for _, vevent := range calendar.Events() {
		for _, event := range eventsFromVEvent(vevent, params.Multiline) {
			if err := csvWriter.Write(event.ToSlice()); err != nil {
				return fmt.Errorf("write CSV event: %w", err)
			}
		}
	}

	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return fmt.Errorf("flush CSV output: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Conversion complete in %s\n", params.OutputFile)
	return nil
}

// main reports conversion errors and exits with a failure status.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
