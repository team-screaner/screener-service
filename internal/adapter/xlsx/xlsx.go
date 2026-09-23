// Package xlsx implements the workbook interchange format for screening matrices.
package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Workbook limits bound upload size, inflated XML and worksheet dimensions.
const (
	MaxFileBytes = 10 << 20
	MaxCellBytes = 32767
	MaxRows      = 2000
	MaxLevels    = 20
)

// Mapping maps header names to category, skill and ordered levels.
type Mapping struct {
	Category string   `json:"category"`
	Skill    string   `json:"skill"`
	Levels   []string `json:"levels"`
}

// Document is the workbook representation of a skill matrix.
type Document struct {
	Name   string   `json:"name"`
	Levels []string `json:"levels"`
	Rows   []Row    `json:"rows"`
}

// Row holds one skill and requirements keyed by level name.
type Row struct {
	Category     string            `json:"category"`
	Skill        string            `json:"skill"`
	Requirements map[string]string `json:"requirements"`
}

// Sheets returns worksheet names after validating the archive limits.
func Sheets(reader io.Reader) (names []string, err error) {
	f, err := open(reader)
	if err != nil {
		return nil, fmt.Errorf("open workbook: %w", err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	return f.GetSheetList(), nil
}

// Preview reads and validates a matrix from a selected worksheet.
func Preview(reader io.Reader, sheet string, mapping Mapping) (doc Document, err error) {
	f, err := open(reader)
	if err != nil {
		return Document{}, fmt.Errorf("open workbook: %w", err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	if sheet == "" {
		sheets := f.GetSheetList()
		if len(sheets) == 0 {
			return Document{}, errors.New("workbook has no sheets")
		}
		sheet = sheets[0]
	}
	rows, err := readRows(f, sheet)
	if err != nil {
		return Document{}, fmt.Errorf("read sheet: %w", err)
	}
	if len(rows) == 0 {
		return Document{}, errors.New("empty sheet")
	}
	mapping, headers, err := resolveMapping(rows[0], mapping)
	if err != nil {
		return Document{}, err
	}
	doc = Document{Name: sheet, Levels: mapping.Levels, Rows: make([]Row, 0, len(rows)-1)}
	seen := make(map[[2]string]bool)
	for i, row := range rows[1:] {
		if blank(row) {
			continue
		}
		if len(row) > len(headers) {
			return Document{}, fmt.Errorf("row %d has cells without headers", i+2)
		}
		result := Row{Category: strings.TrimSpace(value(row, headers[mapping.Category])), Skill: strings.TrimSpace(value(row, headers[mapping.Skill])), Requirements: make(map[string]string)}
		if result.Category == "" || result.Skill == "" {
			return Document{}, fmt.Errorf("row %d requires category and skill", i+2)
		}
		key := [2]string{result.Category, result.Skill}
		if seen[key] {
			return Document{}, fmt.Errorf("row %d duplicates category and skill", i+2)
		}
		seen[key] = true
		for _, level := range doc.Levels {
			result.Requirements[level] = value(row, headers[level])
		}
		doc.Rows = append(doc.Rows, result)
	}
	if len(doc.Rows) == 0 {
		return Document{}, errors.New("sheet contains no skills")
	}
	return doc, nil
}

func resolveMapping(row []string, mapping Mapping) (Mapping, map[string]int, error) {
	mapping.Category = strings.TrimSpace(mapping.Category)
	mapping.Skill = strings.TrimSpace(mapping.Skill)
	if mapping.Category == "" {
		mapping.Category = "Category"
	}
	if mapping.Skill == "" {
		mapping.Skill = "Skill"
	}
	headers := make(map[string]int, len(row))
	for col, raw := range row {
		header := strings.TrimSpace(raw)
		if header == "" {
			return Mapping{}, nil, errors.New("blank column header")
		}
		if _, exists := headers[header]; exists {
			return Mapping{}, nil, fmt.Errorf("duplicate header %q", header)
		}
		headers[header] = col
	}
	if mapping.Category == mapping.Skill {
		return Mapping{}, nil, errors.New("category and skill mappings must differ")
	}
	for _, header := range []string{mapping.Category, mapping.Skill} {
		if _, ok := headers[header]; !ok {
			return Mapping{}, nil, fmt.Errorf("unknown header %q", header)
		}
	}
	if len(mapping.Levels) == 0 {
		for _, raw := range row {
			header := strings.TrimSpace(raw)
			if header != mapping.Category && header != mapping.Skill {
				mapping.Levels = append(mapping.Levels, header)
			}
		}
	} else {
		mapping.Levels = append([]string(nil), mapping.Levels...)
	}
	if len(mapping.Levels) == 0 || len(mapping.Levels) > MaxLevels {
		return Mapping{}, nil, fmt.Errorf("between 1 and %d level mappings are required", MaxLevels)
	}
	seen := map[string]bool{mapping.Category: true, mapping.Skill: true}
	for i, raw := range mapping.Levels {
		level := strings.TrimSpace(raw)
		if _, ok := headers[level]; !ok {
			return Mapping{}, nil, fmt.Errorf("unknown level header %q", level)
		}
		if seen[level] {
			return Mapping{}, nil, fmt.Errorf("duplicate mapping %q", level)
		}
		mapping.Levels[i] = level
		seen[level] = true
	}
	return mapping, headers, nil
}

func blank(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}
func value(row []string, col int) string {
	if col >= len(row) {
		return ""
	}
	return row[col]
}

// Export writes a matrix using literal string cells.
func Export(doc Document) (data []byte, err error) {
	if checkErr := validateDocument(doc); checkErr != nil {
		return nil, checkErr
	}
	f := excelize.NewFile()
	defer func() { err = errors.Join(err, f.Close()) }()
	sheet := strings.TrimSpace(doc.Name)
	if sheet == "" {
		sheet = "Matrix"
	}
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		// Matrix titles are not constrained by Excel's sheet naming rules.
		sheet = "Matrix"
		if checkErr := f.SetSheetName("Sheet1", sheet); checkErr != nil {
			return nil, fmt.Errorf("name sheet: %w", checkErr)
		}
	}
	rows := make([][]string, 0, len(doc.Rows)+1)
	rows = append(rows, append([]string{"Category", "Skill"}, doc.Levels...))
	for _, row := range doc.Rows {
		cells := []string{row.Category, row.Skill}
		for _, level := range doc.Levels {
			cells = append(cells, row.Requirements[level])
		}
		rows = append(rows, cells)
	}
	for y, row := range rows {
		for x, val := range row {
			cell, err := excelize.CoordinatesToCellName(x+1, y+1)
			if err != nil {
				return nil, err
			}
			if checkErr := f.SetCellStr(sheet, cell, val); checkErr != nil {
				return nil, fmt.Errorf("write %s: %w", cell, checkErr)
			}
		}
	}
	var buf bytes.Buffer
	if checkErr := f.Write(&buf); checkErr != nil {
		return nil, fmt.Errorf("encode workbook: %w", checkErr)
	}
	if checkErr := checkArchive(buf.Bytes()); checkErr != nil {
		return nil, checkErr
	}
	return buf.Bytes(), nil
}

// open checks both ZIP metadata and the actual inflated stream, preventing small
// compressed uploads from bypassing the memory budget.
func open(reader io.Reader) (*excelize.File, error) {
	if reader == nil {
		return nil, errors.New("workbook reader is required")
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read workbook: %w", err)
	}
	if len(data) > MaxFileBytes {
		return nil, errors.New("workbook exceeds 10 MiB upload limit")
	}
	if checkErr := checkArchive(data); checkErr != nil {
		return nil, checkErr
	}
	return excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: MaxFileBytes, UnzipXMLSizeLimit: MaxFileBytes, RawCellValue: true})
}

func checkArchive(data []byte) error {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("invalid XLSX archive: %w", err)
	}
	var total uint64
	seen := make(map[string]bool, len(archive.File))
	for _, entry := range archive.File {
		name := strings.ToLower(strings.ReplaceAll(entry.Name, "\\", "/"))
		if seen[name] {
			return errors.New("duplicate workbook archive part")
		}
		seen[name] = true
		if strings.HasSuffix(name, "vbaproject.bin") || strings.Contains(name, "/macrosheets/") {
			return errors.New("macro-enabled workbooks are not supported")
		}
		if entry.UncompressedSize64 > MaxFileBytes-total {
			return errors.New("workbook exceeds 10 MiB uncompressed limit")
		}
		total += entry.UncompressedSize64
	}
	return validateSharedStringReferences(archive)
}

func readRows(f *excelize.File, sheet string) (result [][]string, err error) {
	rows, err := f.Rows(sheet)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		if len(result) > MaxRows {
			return nil, fmt.Errorf("sheet exceeds %d data rows", MaxRows)
		}
		cells, err := rows.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return nil, err
		}
		if len(cells) > MaxLevels+2 {
			return nil, fmt.Errorf("sheet exceeds %d columns", MaxLevels+2)
		}
		for _, cell := range cells {
			if len(cell) > MaxCellBytes {
				return nil, fmt.Errorf("cell exceeds %d bytes", MaxCellBytes)
			}
		}
		result = append(result, cells)
	}
	if checkErr := rows.Error(); checkErr != nil {
		return nil, checkErr
	}
	// Do this after bounded iteration: GetCellFormula materializes the worksheet.
	for y, row := range result {
		for x := range row {
			cell, err := excelize.CoordinatesToCellName(x+1, y+1)
			if err != nil {
				return nil, err
			}
			formula, err := f.GetCellFormula(sheet, cell)
			if err != nil {
				return nil, err
			}
			if formula != "" {
				return nil, fmt.Errorf("formula in %s is not supported", cell)
			}
		}
	}
	return result, nil
}

func validateDocument(doc Document) error {
	totalBytes := 0
	if len(doc.Rows) == 0 || len(doc.Rows) > MaxRows {
		return fmt.Errorf("document requires between 1 and %d rows", MaxRows)
	}
	headers := append([]string{"Category", "Skill"}, doc.Levels...)
	if _, _, err := resolveMapping(headers, Mapping{}); err != nil {
		return err
	}
	seen := make(map[[2]string]bool, len(doc.Rows))
	levels := make(map[string]bool, len(doc.Levels))
	for _, level := range doc.Levels {
		if strings.TrimSpace(level) != level {
			return errors.New("level names must not have surrounding whitespace")
		}
		levels[level] = true
	}
	for i, row := range doc.Rows {
		totalBytes += len(row.Category) + len(row.Skill)
		if totalBytes > MaxFileBytes {
			return errors.New("document text exceeds 10 MiB limit")
		}
		category, skill := strings.TrimSpace(row.Category), strings.TrimSpace(row.Skill)
		if category == "" || skill == "" {
			return fmt.Errorf("row %d requires category and skill", i+2)
		}
		key := [2]string{category, skill}
		if seen[key] {
			return fmt.Errorf("row %d duplicates category and skill", i+2)
		}
		seen[key] = true
		if len(row.Category) > MaxCellBytes || len(row.Skill) > MaxCellBytes {
			return errors.New("category or skill exceeds cell limit")
		}
		for level, value := range row.Requirements {
			totalBytes += len(value)
			if totalBytes > MaxFileBytes {
				return errors.New("document text exceeds 10 MiB limit")
			}
			if !levels[level] {
				return fmt.Errorf("unknown requirement level %q", level)
			}
			if len(value) > MaxCellBytes {
				return fmt.Errorf("cell exceeds %d bytes", MaxCellBytes)
			}
		}
	}
	for _, header := range headers {
		if len(header) > MaxCellBytes {
			return errors.New("header exceeds cell limit")
		}
	}
	return nil
}

// validateSharedStringReferences rejects invalid indexes before any Excelize
// cell reader runs. This guards GO-2026-6452 and parser error swallowing.
func validateSharedStringReferences(archive *zip.Reader) error {
	count := uint64(0)
	for _, entry := range archive.File {
		name := strings.ToLower(strings.ReplaceAll(entry.Name, "\\", "/"))
		if name == "xl/sharedstrings.xml" {
			size, err := inspectWorkbookXML(entry, true, 0)
			if err != nil {
				return err
			}
			count = size
		}
	}
	// Inspect every XML payload, including nonstandard relationship targets
	// whose archive filenames do not end in .xml.
	for _, entry := range archive.File {
		if _, err := inspectWorkbookXML(entry, false, count); err != nil {
			return err
		}
	}
	return nil
}

func inspectWorkbookXML(entry *zip.File, countStrings bool, stringCount uint64) (count uint64, err error) {
	reader, err := entry.Open()
	if err != nil {
		return 0, fmt.Errorf("open workbook XML: %w", err)
	}
	defer func() { err = errors.Join(err, reader.Close()) }()
	data, readErr := io.ReadAll(io.LimitReader(reader, MaxFileBytes+1))
	if readErr != nil {
		return 0, fmt.Errorf("read workbook archive part: %w", readErr)
	}
	if len(data) > MaxFileBytes {
		return 0, errors.New("workbook XML exceeds size limit")
	}
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	if !countStrings && (len(trimmed) == 0 || trimmed[0] != '<') {
		return 0, nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, roots := 0, 0
	for {
		token, tokenErr := decoder.Token()
		if errors.Is(tokenErr, io.EOF) {
			return count, nil
		}
		if tokenErr != nil {
			return 0, fmt.Errorf("invalid workbook XML: %w", tokenErr)
		}
		switch element := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				roots++
				if roots > 1 {
					return 0, errors.New("workbook XML has multiple root elements")
				}
			}
			if countStrings {
				if depth == 1 && element.Name.Local != "sst" {
					return 0, errors.New("invalid shared string table")
				}
				if depth == 2 && element.Name.Local == "si" {
					count++
				}
				continue
			}
			if element.Name.Local != "c" {
				continue
			}
			shared := false
			for _, attribute := range element.Attr {
				if attribute.Name.Local == "t" && attribute.Value == "s" {
					shared = true
				}
			}
			if !shared {
				continue
			}
			var cell struct {
				Value string `xml:"v"`
			}
			if decodeErr := decoder.DecodeElement(&cell, &element); decodeErr != nil {
				return 0, fmt.Errorf("invalid shared string cell: %w", decodeErr)
			}
			depth--
			index, indexErr := strconv.ParseUint(strings.TrimSpace(cell.Value), 10, 64)
			if indexErr != nil || index >= stringCount {
				return 0, errors.New("invalid shared string index")
			}
		case xml.EndElement:
			depth--
		}
	}
}
