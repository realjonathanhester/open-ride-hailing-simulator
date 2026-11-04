// Package csvio holds the default CSV readers and writer.
package csvio

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"

	"github.com/realjonathanhester/ride-hailing-simulator/model"
)

type table struct {
	path   string
	header map[string]int
	rows   [][]string
}

func readTable(path string, required []string) (*table, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("%s: missing header row", path)
	}
	t := &table{path: path, header: map[string]int{}, rows: records[1:]}
	for i, name := range records[0] {
		t.header[name] = i
	}
	for _, name := range required {
		if _, ok := t.header[name]; !ok {
			return nil, fmt.Errorf("%s: missing column %s", path, name)
		}
	}
	return t, nil
}

func (t *table) cell(row []string, name string) string {
	i, ok := t.header[name]
	if !ok || i >= len(row) {
		return ""
	}
	return row[i]
}

func (t *table) float(row []string, line int, name string) (float64, error) {
	v, err := strconv.ParseFloat(t.cell(row, name), 64)
	if err != nil {
		return 0, fmt.Errorf("%s row %d: %s: %w", t.path, line, name, err)
	}
	return v, nil
}

func (t *table) integer(row []string, line int, name string) (int64, error) {
	v, err := strconv.ParseInt(t.cell(row, name), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s row %d: %s: %w", t.path, line, name, err)
	}
	return v, nil
}

func (t *table) optionalInteger(row []string, line int, name string) (*int64, error) {
	if t.cell(row, name) == "" {
		return nil, nil
	}
	v, err := t.integer(row, line, name)
	return &v, err
}

func (t *table) optionalInt(row []string, line int, name string) (*int, error) {
	v, err := t.optionalInteger(row, line, name)
	if v == nil || err != nil {
		return nil, err
	}
	n := int(*v)
	return &n, nil
}

// RequestReader reads ride requests from a CSV file.
type RequestReader struct{ Path string }

// ReadRequests parses every row.
func (r RequestReader) ReadRequests() ([]model.Request, error) {
	t, err := readTable(r.Path, []string{"request_id", "requested_at", "origin_lat", "origin_lng", "destination_lat", "destination_lng"})
	if err != nil {
		return nil, err
	}
	var out []model.Request
	for i, row := range t.rows {
		line := i + 2
		req := model.Request{RequestID: t.cell(row, "request_id")}
		var errs [9]error
		req.RequestedAt, errs[0] = t.integer(row, line, "requested_at")
		req.Origin.Lat, errs[1] = t.float(row, line, "origin_lat")
		req.Origin.Lng, errs[2] = t.float(row, line, "origin_lng")
		req.Destination.Lat, errs[3] = t.float(row, line, "destination_lat")
		req.Destination.Lng, errs[4] = t.float(row, line, "destination_lng")
		req.NumRequestedSeats, errs[5] = t.optionalInt(row, line, "num_requested_seats")
		req.MaxRequestToAssignmentSeconds, errs[6] = t.optionalInteger(row, line, "max_request_to_assignment_seconds")
		req.MaxRequestToPickupSeconds, errs[7] = t.optionalInteger(row, line, "max_request_to_pickup_seconds")
		for _, e := range errs {
			if e != nil {
				return nil, e
			}
		}
		out = append(out, req)
	}
	return out, nil
}

// SupplyReader reads supply from a CSV file.
type SupplyReader struct{ Path string }

// ReadSupply parses every row.
func (r SupplyReader) ReadSupply() ([]model.Supply, error) {
	t, err := readTable(r.Path, []string{"supply_id", "online_at", "online_lat", "online_lng"})
	if err != nil {
		return nil, err
	}
	var out []model.Supply
	for i, row := range t.rows {
		line := i + 2
		s := model.Supply{SupplyID: t.cell(row, "supply_id")}
		var errs [5]error
		s.OnlineAt, errs[0] = t.integer(row, line, "online_at")
		s.OnlineLocation.Lat, errs[1] = t.float(row, line, "online_lat")
		s.OnlineLocation.Lng, errs[2] = t.float(row, line, "online_lng")
		s.OfflineAt, errs[3] = t.optionalInteger(row, line, "offline_at")
		s.NumAvailableSeats, errs[4] = t.optionalInt(row, line, "num_available_seats")
		for _, e := range errs {
			if e != nil {
				return nil, e
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// Writer writes one output stream to a CSV file, streaming rows as they come.
type Writer struct {
	Path string
	file *os.File
	csv  *csv.Writer
}

// Open creates the file and writes the header row.
func (w *Writer) Open(columns []string) error {
	f, err := os.Create(w.Path)
	if err != nil {
		return err
	}
	w.file, w.csv = f, csv.NewWriter(f)
	return w.csv.Write(columns)
}

// Write writes one row.
func (w *Writer) Write(values []string) error { return w.csv.Write(values) }

// Close flushes and closes the file.
func (w *Writer) Close() error {
	w.csv.Flush()
	if err := w.csv.Error(); err != nil {
		w.file.Close()
		return err
	}
	return w.file.Close()
}
