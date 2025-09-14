# ics2csv

A simple Go utility to convert iCalendar (.ics) files to CSV format.

## Features
- Parses .ics files and extracts event details
- Outputs events in CSV format for easy import into spreadsheets
- CLI usage for quick conversion

## Usage

### Help Message

```bash
$ ics2csv --help
Usage: ics2csv --input <input.ics> [--output <output.csv>|stdout]
  -i, --input string    Input ICS file (required)
  -m, --multiline       Preserve newlines and whitespace in fields
  -o, --output string   Output CSV file (default: input name with .csv extension, or 'stdout')
```  

### Example

```bash
$ ics2csv -i input.ics
```
The output will be saved to `input.csv` by default.
You can also specify the output file name or use `stdout` to print to the console.

By default, all fields are converted to a single line (all whitespace and newlines are replaced by a single space).
If you want to preserve newlines and whitespace in fields, use the `--multiline` (or `-m`) flag:

```bash
$ ics2csv -mi input.ics
```

### CSV Output Format
The CSV file contains the following columns:
- Subject
- Start Date
- Start Time
- End Date
- End Time
- Description
- Location

## Installation

### Precompiled executables

You can download the executable for your platform from the [Releases](https://github.com/kpym/ics2csv/releases).

### Compile it yourself

#### Using Go

```
$ go install github.com/kpym/ics2csv@latest
```

#### Using goreleaser

After cloning this repo you can compile the sources with [goreleaser](https://github.com/goreleaser/goreleaser/) for all available platforms:

```
git clone https://github.com/kpym/ics2csv.git .
goreleaser --snapshot --skip-publish --clean
```

You will find the resulting binaries in the `dist/` sub-folder.

### Thanks

This tool is mainly using [github.com/arran4/golang-ical](http://github.com/arran4/golang-ical) to parse the .ics files.

## License
MIT
