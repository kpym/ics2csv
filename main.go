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

	pflag.StringVarP(&csvFile, "output", "o", "", "Output CSV file or 'stdout'(default: input name with .csv extension)")
	pflag.BoolVarP(&multiline, "multiline", "m", false, "Preserve newlines and whitespace in fields")
	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "ics2csv (version %s): convert ICS to CSV\n", version)
		fmt.Fprintf(os.Stderr, "Usage: ics2csv <input.ics> [--output <output.csv>|stdout]\n")
		pflag.PrintDefaults()
	}
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

// parseDateTimeToString parses a raw iCal datetime string using an optional TZID
// and returns the date and time in "YYYY-MM-DD" and "HH:MM:SS" formats.
func parseDateTimeToString(raw string, tz string) (string, string) {
	parsed, err := parseDateTimeToTime(raw, tz)
	if err != nil {
		return "", ""
	}
	return parsed.Format("2006-01-02"), parsed.Format("15:04:05")
}

// parseDateTimeToTime parses raw iCal datetime string into time.Time using optional TZID.
func parseDateTimeToTime(raw string, tz string) (time.Time, error) {
	layouts := []string{"20060102T150405Z07:00", "20060102T1504Z07:00", "20060102T150405", "20060102T1504", "20060102"}
	var parsed time.Time
	var err error
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
	for _, L := range layouts {
		parsed, err = time.Parse(L, raw)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse datetime: %s", raw)
}

// getStartStrings returns the start date and time strings for a VEvent.
func getStartStrings(vevent *ics.VEvent) (string, string) {
	if t, err := vevent.GetStartAt(); err == nil {
		return t.Format("2006-01-02"), t.Format("15:04:05")
	}
	if p := vevent.GetProperty("DTSTART"); p != nil {
		tz := ""
		if v, ok := p.ICalParameters["TZID"]; ok && len(v) > 0 {
			tz = strings.Trim(v[0], "\"")
		}
		return parseDateTimeToString(p.Value, tz)
	}
	return "", ""
}

// getEndStrings returns the end date and time strings for a VEvent.
func getEndStrings(vevent *ics.VEvent) (string, string) {
	if t, err := vevent.GetEndAt(); err == nil {
		return t.Format("2006-01-02"), t.Format("15:04:05")
	}
	if p := vevent.GetProperty("DTEND"); p != nil {
		tz := ""
		if v, ok := p.ICalParameters["TZID"]; ok && len(v) > 0 {
			tz = strings.Trim(v[0], "\"")
		}
		return parseDateTimeToString(p.Value, tz)
	}
	return "", ""
}

// expandRecurrences returns a list of start times for the event, expanding RRULE/RDATE and applying EXDATE.
// If RRULE has no UNTIL/COUNT, expansion is limited to one year from DTSTART.
func expandRecurrences(vevent *ics.VEvent, dtstart time.Time) ([]time.Time, error) {
	var occs []time.Time
	// Collect EXDATEs
	exmap := map[int64]bool{}
	for _, p := range vevent.GetProperties(ics.ComponentPropertyExdate) {
		tz := ""
		if v, ok := p.ICalParameters["TZID"]; ok && len(v) > 0 {
			tz = strings.Trim(v[0], "\"")
		}
		for _, raw := range strings.Split(p.Value, ",") {
			if excluded, err := parseDateTimeToTime(raw, tz); err == nil {
				exmap[excluded.UnixNano()] = true
			}
		}
	}

	// Handle RRULE
	rruleProp := vevent.GetProperty(ics.ComponentPropertyRrule)
	if rruleProp != nil {
		// build rrule string: ensure it has DTSTART context
		ruleStr := rruleProp.Value
		r, err := rrule.StrToRRule(ruleStr)
		if err != nil {
			return nil, err
		}
		// Ensure the rule has the correct DTSTART so generated occurrences have the right time
		// (StrToRRule may not set DTSTART). Set it to the provided dtstart.
		// rrule.RRule exposes SetDtstart in some versions; as a fallback set the Dtstart field if present.
		// set DTSTART on the rrule so generated occurrences keep the event time
		if setter := r.DTStart; setter != nil {
			setter(dtstart)
		}
		// determine window
		windowEnd := dtstart.AddDate(1, 0, 0)
		// if r has an UNTIL or COUNT, Between will respect it; otherwise limit to 1 year
		occs = r.Between(dtstart, windowEnd, true)
	}
	// If no RRULE, use single DTSTART
	if len(occs) == 0 {
		occs = []time.Time{dtstart}
	}
	// Filter EXDATEs
	var filtered []time.Time
	for _, o := range occs {
		if !exmap[o.UnixNano()] {
			filtered = append(filtered, o)
		}
	}
	return filtered, nil
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
		summaryProp := vevent.GetProperty(ics.ComponentPropertySummary)
		if summaryProp == nil {
			continue
		}
		subj := normalize(summaryProp.Value)
		desc := ""
		if p := vevent.GetProperty(ics.ComponentPropertyDescription); p != nil {
			desc = normalize(p.Value)
		}
		loc := ""
		if p := vevent.GetProperty(ics.ComponentPropertyLocation); p != nil {
			loc = normalize(p.Value)
		}

		// Try to get original start/end times as time.Time
		startT, startErr := vevent.GetStartAt()
		endT, endErr := vevent.GetEndAt()
		duration := time.Duration(0)
		// If GetEndAt failed, try to parse DTEND property (handles quoted TZID)
		if endErr != nil {
			if p := vevent.GetProperty("DTEND"); p != nil {
				tz := ""
				if v, ok := p.ICalParameters["TZID"]; ok && len(v) > 0 {
					tz = strings.Trim(v[0], "\"")
				}
				if t, err := parseDateTimeToTime(p.Value, tz); err == nil {
					endT = t
					endErr = nil
				}
			}
		}
		// If GetStartAt failed but DTSTART parsed earlier (in RRULE branch), duration may still be zero.
		// If endT is available and startT parsed, compute duration.
		if startErr == nil && endErr == nil {
			duration = endT.Sub(startT)
		}

		// If RRULE present, expand occurrences. If library couldn't parse DTSTART,
		// try to parse it ourselves (handles quoted TZID like TZID="Europe/Brussels").
		if vevent.HasProperty(ics.ComponentPropertyRrule) {
			if startErr != nil {
				// attempt to parse DTSTART property
				if p := vevent.GetProperty("DTSTART"); p != nil {
					tz := ""
					if v, ok := p.ICalParameters["TZID"]; ok && len(v) > 0 {
						tz = strings.Trim(v[0], "\"")
					}
					if t, err := parseDateTimeToTime(p.Value, tz); err == nil {
						startT = t
						startErr = nil
					}
				}
			}
			// recompute duration if we now have both start and end
			if startErr == nil && endErr == nil {
				duration = endT.Sub(startT)
			}
			if startErr == nil {
				occs, err := expandRecurrences(vevent, startT)
				if err != nil {
					// fallback to single occurrence
					occs = []time.Time{startT}
				}
				for _, occ := range occs {
					ev := Event{
						Subject:     subj,
						Description: desc,
						Location:    loc,
					}
					ev.StartDate = occ.Format("2006-01-02")
					ev.StartTime = occ.Format("15:04:05")
					if duration != 0 {
						e := occ.Add(duration)
						ev.EndDate = e.Format("2006-01-02")
						ev.EndTime = e.Format("15:04:05")
					} else if endErr == nil {
						ev.EndDate = endT.Format("2006-01-02")
						ev.EndTime = endT.Format("15:04:05")
					}
					csvw.Write(ev.ToSlice())
				}
				continue
			}
			// if still no valid startT, fall through to non-recurring handling
		}

		// Non-recurring or RRULE but no start time: fall back to simple extraction
		var event Event
		event.Subject = subj
		event.Description = desc
		event.Location = loc
		event.StartDate, event.StartTime = getStartStrings(vevent)
		event.EndDate, event.EndTime = getEndStrings(vevent)
		if event.Subject != "" {
			csvw.Write(event.ToSlice())
		}
	}

	fmt.Println("Conversion complete in", params.OutputFile)
}
