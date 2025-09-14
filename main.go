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
)

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

func rawTimeParse(raw string) (dateStr, timeStr string, err error) {
	const icsLayout = "20060102T150405"

	if raw == "" {
		return
	}

	if t, err := time.Parse(icsLayout, raw); err == nil {
		dateStr = t.Format("2006-01-02")
		timeStr = t.Format("15:04")
	}
	return
}

func main() {
	var (
		err     error         // temp error variable
		icsFile string        // input ICS file name
		csvFile string        // output CSV file name
		icsf    *os.File      // input ICS file handle
		csvf    *os.File      // output CSV file handle
		cal     *ics.Calendar // parsed calendar
		csvw    *csv.Writer   // CSV writer
	)

	// get the file names from command line arguments
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Println("Usage: ics2csv <input.ics> [<output.csv>|stdout]")
		os.Exit(1)
	}
	icsFile = os.Args[1]
	if len(os.Args) == 3 {
		csvFile = os.Args[2]
	} else {
		// use input name with .csv extension
		csvFile = strings.TrimSuffix(icsFile, ".ics") + ".csv"
	}

	// Open and parse the ICS file
	icsf, err = os.Open(icsFile)
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
	if csvFile == "stdout" {
		csvf = os.Stdout
	} else {
		csvf, err = os.Create(csvFile)
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
	for _, comp := range cal.Components {
		var event Event
		var startRaw, endRaw string
		for _, prop := range comp.UnknownPropertiesIANAProperties() {
			switch prop.IANAToken {
			case "SUMMARY":
				event.Subject = prop.Value
			case "DTSTART":
				startRaw = prop.Value
			case "DTEND":
				endRaw = prop.Value
			case "DESCRIPTION":
				event.Description = prop.Value
			case "LOCATION":
				event.Location = prop.Value
			}
		}
		// Parse start and end times
		if event.StartDate, event.StartTime, err = rawTimeParse(startRaw); err != nil {
			fmt.Printf("Error parsing start time: %v\n", err)
		}
		if event.EndDate, event.EndTime, err = rawTimeParse(endRaw); err != nil {
			fmt.Printf("Error parsing end time: %v\n", err)
		}
		// Write event if it has a subject
		if event.Subject != "" {
			csvw.Write(event.ToSlice())
		}
	}

	fmt.Println("Conversion complete in", csvFile)
}
