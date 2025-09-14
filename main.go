// ics2csv is a small tool that convert
// an iCalendar (.ics) file to a CSV file.
package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"github.com/arran4/golang-ical"
	"github.com/spf13/pflag"
)

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

	pflag.StringVarP(&icsFile, "input", "i", "", "Input ICS file (required)")
	pflag.StringVarP(&csvFile, "output", "o", "", "Output CSV file (default: input name with .csv extension, or 'stdout')")
	pflag.BoolVarP(&multiline, "multiline", "m", false, "Preserve newlines and whitespace in fields")
	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: ics2csv --input <input.ics> [--output <output.csv>|stdout]\n")
		pflag.PrintDefaults()
	}
	pflag.Parse()

	if icsFile == "" {
		pflag.Usage()
		os.Exit(1)
	}
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

// toSingleLine converts a string to a single line by replacing all
// groups of whitespaces (including newlines) with a single space.
// Used when --oneliner flag is set.
func toSingleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// doNothing returns the string unchanged.
// Used when --oneliner flag is not set.
func doNothing(s string) string {
	return s
}

func main() {
	var (
		err  error         // temp error variable
		icsf *os.File      // input ICS file handle
		csvf *os.File      // output CSV file handle
		cal  *ics.Calendar // parsed calendar
		csvw *csv.Writer   // CSV writer
	)
	// Read command-line parameters
	params := ParseFlags()
	normalize := toSingleLine
	if params.Multiline {
		normalize = doNothing
	}
	// Open and parse the ICS file
	icsf, err = os.Open(params.InputFile)
	if err != nil {
		fmt.Printf("Error opening ICS file: %v\n", err)
		os.Exit(1)
	}
	defer icsf.Close()

	cal, err = ics.ParseCalendar(icsf)
	if err != nil {
		fmt.Printf("Error parsing ICS file: %v\n", err)
		os.Exit(1)
	}

	// Open the CSV file for writing
	if params.OutputFile == "stdout" {
		csvf = os.Stdout
	} else {
		csvf, err = os.Create(params.OutputFile)
		if err != nil {
			fmt.Printf("Error creating CSV file: %v\n", err)
			os.Exit(1)
		}
		defer csvf.Close()
	}

	csvw = csv.NewWriter(csvf)
	defer csvw.Flush()

	// Write header
	csvw.Write(Header())

	// Write events
	for _, vevent := range cal.Events() {
		var event Event
		if p := vevent.GetProperty(ics.ComponentPropertySummary); p != nil {
			event.Subject = normalize(p.Value)
		}
		if p := vevent.GetProperty(ics.ComponentPropertyDescription); p != nil {
			event.Description = normalize(p.Value)
		}
		if p := vevent.GetProperty(ics.ComponentPropertyLocation); p != nil {
			event.Location = normalize(p.Value)
		}
		if t, e := vevent.GetStartAt(); e == nil {
			event.StartDate = t.Format("2006-01-02")
			event.StartTime = t.Format("15:04:05")
		}
		if t, e := vevent.GetEndAt(); e == nil {
			event.EndDate = t.Format("2006-01-02")
			event.EndTime = t.Format("15:04:05")
		}
		// Write event if it has a subject
		if event.Subject != "" {
			csvw.Write(event.ToSlice())
		}
	}

	fmt.Println("Conversion complete in", params.OutputFile)
}
