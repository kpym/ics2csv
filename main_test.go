package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/spf13/pflag"
)

// TestCSVHelpers checks the event row and header column ordering.
func TestCSVHelpers(t *testing.T) {
	event := Event{
		Subject:     "Subject",
		StartDate:   "2024-10-26",
		StartTime:   "12:00:00",
		EndDate:     "2024-10-26",
		EndTime:     "13:00:00",
		Description: "Description",
		Location:    "Location",
	}
	wantRow := []string{"Subject", "2024-10-26", "12:00:00", "2024-10-26", "13:00:00", "Description", "Location"}
	if !reflect.DeepEqual(event.ToSlice(), wantRow) {
		t.Errorf("ToSlice() = %#v, want %#v", event.ToSlice(), wantRow)
	}

	wantHeader := []string{"Subject", "Start Date", "Start Time", "End Date", "End Time", "Description", "Location"}
	if !reflect.DeepEqual(Header(), wantHeader) {
		t.Errorf("Header() = %#v, want %#v", Header(), wantHeader)
	}
}

// TestParseFlags checks default output naming and explicit CLI options.
func TestParseFlags(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantOutput string
		wantMulti  bool
	}{
		{name: "default output", args: []string{"meeting.ics"}, wantOutput: "meeting.csv"},
		{
			name:       "explicit output and multiline",
			args:       []string{"meeting.ics", "--output", "stdout", "--multiline"},
			wantOutput: "stdout",
			wantMulti:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setCommandLineArgs(t, test.args...)
			got := ParseFlags()
			if got.InputFile != "meeting.ics" || got.OutputFile != test.wantOutput || got.Multiline != test.wantMulti {
				t.Errorf("ParseFlags() = %#v", got)
			}
		})
	}
}

// TestTextAndDateHelpers covers text normalization, TZID extraction, and formatting.
func TestTextAndDateHelpers(t *testing.T) {
	if got := normalizeText(" first\n second  ", false); got != "first second" {
		t.Errorf("normalizeText() = %q, want %q", got, "first second")
	}
	if got := normalizeText("first\nsecond", true); got != "first\nsecond" {
		t.Errorf("normalizeText() with multiline = %q, want original text", got)
	}
	if got := getTZID(nil); got != "" {
		t.Errorf("getTZID(nil) = %q, want empty string", got)
	}
	if got := getTZID(map[string][]string{"TZID": {`"Europe/Brussels"`, "ignored"}}); got != "Europe/Brussels" {
		t.Errorf("getTZID() = %q, want Europe/Brussels", got)
	}

	value := time.Date(2024, time.October, 26, 12, 34, 56, 0, time.UTC)
	if date, clock := formatDateTime(value, nil); date != "2024-10-26" || clock != "12:34:56" {
		t.Errorf("formatDateTime() = (%q, %q), want (2024-10-26, 12:34:56)", date, clock)
	}
	if date, clock := formatDateTime(value, errors.New("invalid")); date != "" || clock != "" {
		t.Errorf("formatDateTime() on error = (%q, %q), want empty strings", date, clock)
	}

	vevent := parseTestVEvent(t,
		"UID:text@example.com",
		"DTSTAMP:20241026T000000Z",
		"SUMMARY: Summary",
		"DESCRIPTION: Bring notes",
	)
	if got := propertyText(vevent, ics.ComponentPropertyDescription, false); got != "Bring notes" {
		t.Errorf("propertyText() = %q, want Bring notes", got)
	}
	if got := propertyText(vevent, ics.ComponentPropertyLocation, false); got != "" {
		t.Errorf("propertyText() for missing property = %q, want empty string", got)
	}
}

// TestParseDateTimeToTime covers supported formats and invalid input.
func TestParseDateTimeToTime(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		tz       string
		wantDate string
		wantTime string
	}{
		{
			name:     "date and seconds",
			raw:      "20241026T123456",
			wantDate: "2024-10-26",
			wantTime: "12:34:56",
		},
		{
			name:     "date only",
			raw:      "20241026",
			wantDate: "2024-10-26",
			wantTime: "00:00:00",
		},
		{
			name:     "UTC suffix",
			raw:      "20241026T123456Z",
			wantDate: "2024-10-26",
			wantTime: "12:34:56",
		},
		{
			name:     "named timezone",
			raw:      "20241026T123456",
			tz:       "UTC",
			wantDate: "2024-10-26",
			wantTime: "12:34:56",
		},
		{
			name:     "numeric offset",
			raw:      "20241026T123456+02:00",
			wantDate: "2024-10-26",
			wantTime: "12:34:56",
		},
		{
			name: "invalid datetime",
			raw:  "not-a-date",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseDateTimeToTime(test.raw, test.tz)
			if test.wantDate == "" {
				if err == nil {
					t.Fatal("parseDateTimeToTime succeeded for invalid input")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDateTimeToTime(%q, %q): %v", test.raw, test.tz, err)
			}
			gotDate, gotTime := parsed.Format("2006-01-02"), parsed.Format("15:04:05")
			if gotDate != test.wantDate || gotTime != test.wantTime {
				t.Errorf("parseDateTimeToTime(%q, %q) = (%q, %q), want (%q, %q)",
					test.raw, test.tz, gotDate, gotTime, test.wantDate, test.wantTime)
			}
		})
	}
}

// TestGetDateTimeFallsBackToProperty verifies raw-property datetime parsing.
func TestGetDateTimeFallsBackToProperty(t *testing.T) {
	vevent := parseTestVEvent(t,
		"UID:test@example.com",
		"DTSTAMP:20241026T000000Z",
		"DTSTART;TZID=UTC:20241026T123456",
	)
	want := time.Date(2024, time.October, 26, 12, 34, 56, 0, time.UTC)

	got, err := getDateTime(vevent, "DTSTART", func() (time.Time, error) {
		return want, nil
	})
	if err != nil || !got.Equal(want) {
		t.Fatalf("getDateTime() with parsed value = %s, %v; want %s, nil", got, err, want)
	}

	got, err = getDateTime(vevent, "DTSTART", func() (time.Time, error) {
		return time.Time{}, errors.New("force raw property fallback")
	})
	if err != nil {
		t.Fatalf("getDateTime: %v", err)
	}

	if !got.Equal(want) {
		t.Errorf("getDateTime() = %s, want %s", got, want)
	}

	fallbackErr := errors.New("original parse error")
	_, err = getDateTime(vevent, "MISSING", func() (time.Time, error) {
		return time.Time{}, fallbackErr
	})
	if !errors.Is(err, fallbackErr) {
		t.Errorf("getDateTime() error = %v, want original error", err)
	}
}

// TestExpandRecurrences covers single events, EXDATE filtering, and invalid rules.
func TestExpandRecurrences(t *testing.T) {
	start := time.Date(2024, time.October, 26, 12, 0, 0, 0, time.UTC)
	single := parseTestVEvent(t,
		"UID:single@example.com",
		"DTSTAMP:20241026T000000Z",
		"DTSTART:20241026T120000Z",
	)
	got, err := expandRecurrences(single, start)
	if err != nil || !reflect.DeepEqual(got, []time.Time{start}) {
		t.Fatalf("expandRecurrences(single) = %v, %v; want DTSTART only", got, err)
	}

	recurring := parseTestVEvent(t,
		"UID:recurring@example.com",
		"DTSTAMP:20241026T000000Z",
		"DTSTART;TZID=UTC:20241026T120000",
		"RRULE:FREQ=DAILY;COUNT=3",
		"EXDATE;TZID=UTC:20241027T120000",
	)
	got, err = expandRecurrences(recurring, start)
	want := []time.Time{
		start,
		start.AddDate(0, 0, 2),
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("expandRecurrences() = %v, %v; want %v", got, err, want)
	}

	invalidRule := parseTestVEvent(t,
		"UID:invalid@example.com",
		"DTSTAMP:20241026T000000Z",
		"DTSTART:20241026T120000Z",
		"RRULE:INVALID",
	)
	if _, err := expandRecurrences(invalidRule, start); err == nil {
		t.Error("expandRecurrences() accepted an invalid RRULE")
	}
}

// TestEventsFromVEvent covers text normalization and recurring event expansion.
func TestEventsFromVEvent(t *testing.T) {
	tests := []struct {
		name       string
		properties []string
		want       []Event
	}{
		{
			name: "single event normalizes text",
			properties: []string{
				"UID:single@example.com",
				"DTSTAMP:20241026T000000Z",
				"DTSTART;TZID=UTC:20241026T123456",
				"DTEND;TZID=UTC:20241026T133456",
				"SUMMARY:Team   sync",
				"DESCRIPTION:Bring   notes",
				"LOCATION:Room  A",
			},
			want: []Event{{
				Subject:     "Team sync",
				StartDate:   "2024-10-26",
				StartTime:   "12:34:56",
				EndDate:     "2024-10-26",
				EndTime:     "13:34:56",
				Description: "Bring notes",
				Location:    "Room A",
			}},
		},
		{
			name: "recurring event expands into rows",
			properties: []string{
				"UID:recurring@example.com",
				"DTSTAMP:20241026T000000Z",
				"DTSTART;TZID=UTC:20241026T120000",
				"DTEND;TZID=UTC:20241026T130000",
				"RRULE:FREQ=DAILY;COUNT=2",
				"SUMMARY:Daily",
			},
			want: []Event{
				{Subject: "Daily", StartDate: "2024-10-26", StartTime: "12:00:00", EndDate: "2024-10-26", EndTime: "13:00:00"},
				{Subject: "Daily", StartDate: "2024-10-27", StartTime: "12:00:00", EndDate: "2024-10-27", EndTime: "13:00:00"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := eventsFromVEvent(parseTestVEvent(t, test.properties...), false)
			if len(got) != len(test.want) {
				t.Fatalf("got %d events, want %d", len(got), len(test.want))
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Errorf("event %d = %#v, want %#v", i, got[i], test.want[i])
				}
			}
		})
	}

	missingSummary := parseTestVEvent(t,
		"UID:no-summary@example.com",
		"DTSTAMP:20241026T000000Z",
		"DTSTART:20241026T120000Z",
	)
	if got := eventsFromVEvent(missingSummary, false); len(got) != 0 {
		t.Errorf("eventsFromVEvent() without SUMMARY = %#v, want no rows", got)
	}
}

// TestRunReturnsErrorOnMissingInput verifies that run reports input open failures.
func TestRunReturnsErrorOnMissingInput(t *testing.T) {
	missingInput := filepath.Join(t.TempDir(), "missing.ics")
	setCommandLineArgs(t, missingInput)
	if err := run(); err == nil {
		t.Fatal("run() succeeded with a missing input file")
	}
}

// TestMainConvertsInput exercises the CLI entry point with temporary files.
func TestMainConvertsInput(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "meeting.ics")
	outputPath := filepath.Join(directory, "meeting.csv")
	calendar := testCalendar(
		"UID:meeting@example.com",
		"DTSTAMP:20241026T000000Z",
		"DTSTART:20241026T120000Z",
		"DTEND:20241026T130000Z",
		"SUMMARY:Meeting",
	)
	if err := os.WriteFile(inputPath, []byte(calendar), 0o600); err != nil {
		t.Fatalf("write input calendar: %v", err)
	}

	setCommandLineArgs(t, inputPath, "--output", outputPath)
	main()

	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output CSV: %v", err)
	}
	want := "Subject,Start Date,Start Time,End Date,End Time,Description,Location\n" +
		"Meeting,2024-10-26,12:00:00,2024-10-26,13:00:00,,\n"
	if string(got) != want {
		t.Errorf("CSV output = %q, want %q", got, want)
	}
}

// setCommandLineArgs isolates package-level CLI state for a test.
func setCommandLineArgs(t *testing.T, args ...string) {
	t.Helper()
	oldArgs := os.Args
	oldFlagSet := pflag.CommandLine
	os.Args = append([]string{"ics2csv"}, args...)
	pflag.CommandLine = pflag.NewFlagSet("ics2csv", pflag.ContinueOnError)
	pflag.CommandLine.SetOutput(io.Discard)
	t.Cleanup(func() {
		os.Args = oldArgs
		pflag.CommandLine = oldFlagSet
	})
}

// parseTestVEvent builds a calendar containing one event from the supplied properties.
func parseTestVEvent(t *testing.T, properties ...string) *ics.VEvent {
	t.Helper()
	calendar, err := ics.ParseCalendar(strings.NewReader(testCalendar(properties...)))
	if err != nil {
		t.Fatalf("ParseCalendar: %v", err)
	}
	events := calendar.Events()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	return events[0]
}

// testCalendar wraps event properties in a minimal iCalendar document.
func testCalendar(properties ...string) string {
	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//test//ics2csv//EN",
		"BEGIN:VEVENT",
	}
	lines = append(lines, properties...)
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")
	return strings.Join(lines, "\r\n") + "\r\n"
}
