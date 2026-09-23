package xlsx_test

import (
	"archive/zip"
	"bytes"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"

	adapter "github.com/team-screaner/screener-service/internal/adapter/xlsx"
	"github.com/xuri/excelize/v2"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	want := adapter.Document{Name: "Backend", Levels: []string{"Junior", "Senior"}, Rows: []adapter.Row{
		{Category: "Go", Skill: "Concurrency", Requirements: map[string]string{"Junior": "Goroutines", "Senior": "Memory model\nRace detection"}},
		{Category: "Базы данных", Skill: "SQL", Requirements: map[string]string{"Junior": "SELECT", "Senior": ""}},
	}}
	data, err := adapter.Export(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := adapter.Preview(bytes.NewReader(data), "", adapter.Mapping{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	sheets, err := adapter.Sheets(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sheets, []string{"Backend"}) {
		t.Fatalf("sheets = %v", sheets)
	}
}

func workbook(t *testing.T, rows [][]string) *excelize.File {
	t.Helper()
	f := excelize.NewFile()
	t.Cleanup(func() {
		if checkErr := f.Close(); checkErr != nil {
			t.Error(checkErr)
		}
	})
	for y, row := range rows {
		for x, val := range row {
			cell, err := excelize.CoordinatesToCellName(x+1, y+1)
			if err != nil {
				t.Fatal(err)
			}
			if checkErr := f.SetCellStr("Sheet1", cell, val); checkErr != nil {
				t.Fatal(checkErr)
			}
		}
	}
	return f
}

func encoded(t *testing.T, f *excelize.File) *bytes.Reader {
	t.Helper()
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestPreviewMappingAndSheetSelection(t *testing.T) {
	t.Parallel()
	f := workbook(t, [][]string{{"Категория", "Навык", "Начальный", "Эксперт"}, {"Go", "Channels", "send", "select"}})
	if _, err := f.NewSheet("Empty"); err != nil {
		t.Fatal(err)
	}
	got, err := adapter.Preview(encoded(t, f), "Sheet1", adapter.Mapping{Category: "Категория", Skill: "Навык", Levels: []string{"Эксперт"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows[0].Requirements["Эксперт"] != "select" || len(got.Levels) != 1 {
		t.Fatalf("unexpected preview: %#v", got)
	}
	for _, sheet := range []string{"Empty", "Absent"} {
		if _, err := adapter.Preview(encoded(t, f), sheet, adapter.Mapping{}); err == nil {
			t.Errorf("sheet %q accepted", sheet)
		}
	}
}

func TestInvalidMappingsAndRows(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		rows    [][]string
		mapping adapter.Mapping
	}{
		{name: "missing category header", rows: [][]string{{"Skill", "Junior"}, {"SQL", "select"}}},
		{name: "duplicate header", rows: [][]string{{"Category", "Skill", "Junior", "Junior"}, {"DB", "SQL", "x", "y"}}},
		{name: "blank header", rows: [][]string{{"Category", "Skill", "", "Junior"}, {"DB", "SQL", "x", "y"}}},
		{name: "no levels", rows: [][]string{{"Category", "Skill"}, {"DB", "SQL"}}},
		{name: "unknown mapped header", rows: [][]string{{"Category", "Skill", "Junior"}, {"DB", "SQL", "x"}}, mapping: adapter.Mapping{Levels: []string{"Senior"}}},
		{name: "duplicate level mapping", rows: [][]string{{"Category", "Skill", "Junior"}, {"DB", "SQL", "x"}}, mapping: adapter.Mapping{Levels: []string{"Junior", "Junior"}}},
		{name: "overlapping mappings", rows: [][]string{{"Category", "Skill", "Junior"}, {"DB", "SQL", "x"}}, mapping: adapter.Mapping{Category: "Skill"}},
		{name: "reserved level mapping", rows: [][]string{{"Category", "Skill", "Junior"}, {"DB", "SQL", "x"}}, mapping: adapter.Mapping{Levels: []string{"Skill"}}},
		{name: "empty category", rows: [][]string{{"Category", "Skill", "Junior"}, {" ", "SQL", "x"}}},
		{name: "empty skill", rows: [][]string{{"Category", "Skill", "Junior"}, {"DB", " ", "x"}}},
		{name: "duplicate skill", rows: [][]string{{"Category", "Skill", "Junior"}, {"DB", "SQL", "x"}, {" DB ", "SQL ", "y"}}},
		{name: "row beyond headers", rows: [][]string{{"Category", "Skill", "Junior"}, {"DB", "SQL", "x", "unexpected"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := workbook(t, tc.rows)
			if _, err := adapter.Preview(encoded(t, f), "", tc.mapping); err == nil {
				t.Fatal("invalid workbook accepted")
			}
		})
	}
}

func TestRejectFormulas(t *testing.T) {
	t.Parallel()
	for _, cell := range []string{"A1", "A2", "B2", "C2", "D2", "C3"} {
		t.Run(cell, func(t *testing.T) {
			t.Parallel()
			f := workbook(t, [][]string{{"Category", "Skill", "Junior"}, {"Go", "Channels", "send"}})
			if checkErr := f.SetCellFormula("Sheet1", cell, `HYPERLINK("https://evil.invalid","click")`); checkErr != nil {
				t.Fatal(checkErr)
			}
			if _, err := adapter.Preview(encoded(t, f), "", adapter.Mapping{}); err == nil {
				t.Fatal("formula accepted")
			}
		})
	}
}

func TestExportFormulaLookingStringsAreLiteral(t *testing.T) {
	t.Parallel()
	for _, literal := range []string{"=1+1", "+cmd|' /C calc'!A0", "-1+1", "@SUM(A1)", "\t=1+1"} {
		t.Run(literal, func(t *testing.T) {
			t.Parallel()
			data, err := adapter.Export(adapter.Document{Name: "Matrix", Levels: []string{"Junior"}, Rows: []adapter.Row{{Category: "Go", Skill: "Channels", Requirements: map[string]string{"Junior": literal}}}})
			if err != nil {
				t.Fatal(err)
			}
			f, err := excelize.OpenReader(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if checkErr := f.Close(); checkErr != nil {
					t.Error(checkErr)
				}
			}()
			formula, err := f.GetCellFormula("Matrix", "C2")
			if err != nil {
				t.Fatal(err)
			}
			if formula != "" {
				t.Fatalf("export created formula %q", formula)
			}
			value, err := f.GetCellValue("Matrix", "C2")
			if err != nil {
				t.Fatal(err)
			}
			if value != literal {
				t.Fatalf("literal changed to %q", value)
			}
		})
	}
}

func TestLimits(t *testing.T) {
	t.Parallel()
	t.Run("upload", func(t *testing.T) {
		t.Parallel()
		if _, err := adapter.Sheets(bytes.NewReader(bytes.Repeat([]byte("x"), 10*1024*1024+1))); err == nil {
			t.Fatal("oversized upload accepted")
		}
	})
	t.Run("cell", func(t *testing.T) {
		t.Parallel()
		f := workbook(t, [][]string{{"Category", "Skill", "Junior"}, {"Go", "Channels", strings.Repeat("я", 20000)}})
		if _, err := adapter.Preview(encoded(t, f), "", adapter.Mapping{}); err == nil {
			t.Fatal("oversized cell accepted")
		}
	})
	t.Run("rows", func(t *testing.T) {
		t.Parallel()
		rows := make([][]string, 1, 2002)
		rows[0] = []string{"Category", "Skill", "Junior"}
		for i := range 2001 {
			rows = append(rows, []string{"Go", strconv.Itoa(i), "req"})
		}
		if _, err := adapter.Preview(encoded(t, workbook(t, rows)), "", adapter.Mapping{}); err == nil {
			t.Fatal("too many rows accepted")
		}
	})
	t.Run("levels", func(t *testing.T) {
		t.Parallel()
		headers := make([]string, 2, 23)
		headers[0], headers[1] = "Category", "Skill"
		row := make([]string, 2, 23)
		row[0], row[1] = "Go", "Channels"
		for i := range 21 {
			headers = append(headers, strconv.Itoa(i))
			row = append(row, "req")
		}
		if _, err := adapter.Preview(encoded(t, workbook(t, [][]string{headers, row})), "", adapter.Mapping{}); err == nil {
			t.Fatal("too many levels accepted")
		}
	})
	t.Run("uncompressed", func(t *testing.T) {
		t.Parallel()
		f := workbook(t, [][]string{{"Category", "Skill", "Junior"}, {"Go", "Channels", "req"}})
		data := addZIPEntry(t, encoded(t, f), "oversized.txt", bytes.Repeat([]byte("x"), 10*1024*1024))
		if _, err := adapter.Sheets(bytes.NewReader(data)); err == nil {
			t.Fatal("zip bomb accepted")
		}
	})
	t.Run("macros", func(t *testing.T) {
		t.Parallel()
		f := workbook(t, [][]string{{"Category", "Skill", "Junior"}, {"Go", "Channels", "req"}})
		data := addZIPEntry(t, encoded(t, f), "xl/vbaProject.bin", []byte("macro"))
		if _, err := adapter.Sheets(bytes.NewReader(data)); err == nil {
			t.Fatal("macro-enabled workbook accepted")
		}
	})
}

func addZIPEntry(t *testing.T, source *bytes.Reader, name string, data []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(source, source.Size())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, entry := range zr.File {
		if checkErr := zw.Copy(entry); checkErr != nil {
			t.Fatal(checkErr)
		}
	}
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if checkErr := zw.Close(); checkErr != nil {
		t.Fatal(checkErr)
	}
	return buf.Bytes()
}

func TestExportValidation(t *testing.T) {
	t.Parallel()
	valid := func() adapter.Document {
		return adapter.Document{Name: "Matrix", Levels: []string{"Junior"}, Rows: []adapter.Row{{Category: "Go", Skill: "Channels", Requirements: map[string]string{"Junior": "send"}}}}
	}
	cases := []struct {
		name   string
		change func(*adapter.Document)
	}{
		{"empty rows", func(d *adapter.Document) { d.Rows = nil }},
		{"missing levels", func(d *adapter.Document) { d.Levels = nil }},
		{"duplicate levels", func(d *adapter.Document) { d.Levels = []string{"Junior", "Junior"} }},
		{"unknown requirement level", func(d *adapter.Document) { d.Rows[0].Requirements["Ghost"] = "x" }},
		{"empty skill", func(d *adapter.Document) { d.Rows[0].Skill = " " }},
		{"duplicate rows", func(d *adapter.Document) { d.Rows = append(d.Rows, d.Rows[0]) }},
		{"oversized category aggregate", func(d *adapter.Document) {
			d.Rows = nil
			for i := range 400 {
				d.Rows = append(d.Rows, adapter.Row{Category: strings.Repeat("x", 32767), Skill: strconv.Itoa(i)})
			}
		}},
		{"oversized aggregate", func(d *adapter.Document) {
			d.Rows = nil
			for i := range 400 {
				d.Rows = append(d.Rows, adapter.Row{Category: "Go", Skill: strconv.Itoa(i), Requirements: map[string]string{"Junior": strings.Repeat("x", 32767)}})
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := valid()
			tc.change(&doc)
			if _, err := adapter.Export(doc); err == nil {
				t.Fatal("invalid document exported")
			}
		})
	}
}

func TestPreviewRejectsSparseFarColumn(t *testing.T) {
	t.Parallel()
	f := workbook(t, [][]string{{"Category", "Skill", "Junior"}, {"Go", "Channels", "send"}})
	if checkErr := f.SetCellStr("Sheet1", "XFD1", "Unused"); checkErr != nil {
		t.Fatal(checkErr)
	}
	if _, err := adapter.Preview(encoded(t, f), "", adapter.Mapping{Levels: []string{"Junior"}}); err == nil {
		t.Fatal("extreme sparse column accepted")
	}
}

func TestExportFallsBackToMatrixSheetName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", strings.Repeat("Б", 32), "Backend / Go", "Backend [Senior]"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := adapter.Export(adapter.Document{Name: name, Levels: []string{"Junior"}, Rows: []adapter.Row{{Category: "Go", Skill: "Channels", Requirements: map[string]string{"Junior": "send"}}}})
			if err != nil {
				t.Fatal(err)
			}
			names, err := adapter.Sheets(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(names, []string{"Matrix"}) {
				t.Fatalf("sheets = %v", names)
			}
		})
	}
}

func TestPreviewRejectsNegativeSharedStringIndexWithoutPanic(t *testing.T) {
	t.Parallel()
	f := workbook(t, [][]string{{"Category", "Skill", "Junior"}, {"Go", "Channels", "send"}})
	malformed := rewriteWorksheetXML(t, encoded(t, f), func(xml string) string { return strings.Replace(xml, "<v>5</v>", "<v>-1</v>", 1) })
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatal("Preview panicked instead of rejecting negative shared-string index (GO-2026-6452)")
		}
	}()
	if _, err := adapter.Preview(bytes.NewReader(malformed), "", adapter.Mapping{}); err == nil {
		t.Fatal("negative shared-string index accepted")
	}
}

func rewriteWorksheetXML(t *testing.T, source *bytes.Reader, rewrite func(string) string) []byte {
	t.Helper()
	archive, archiveErr := zip.NewReader(source, source.Size())
	if archiveErr != nil {
		t.Fatal(archiveErr)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, entry := range archive.File {
		if entry.Name != "xl/worksheets/sheet1.xml" {
			if copyErr := writer.Copy(entry); copyErr != nil {
				t.Fatal(copyErr)
			}
			continue
		}
		reader, openErr := entry.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read worksheet fixture: %v %v", readErr, closeErr)
		}
		target, createErr := writer.Create(entry.Name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := io.WriteString(target, rewrite(string(data))); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	return output.Bytes()
}

func TestSharedStringReferenceBoundary(t *testing.T) {
	t.Parallel()
	for _, index := range []string{"6", "999999", "18446744073709551616", "NaN", "5.0", "", "-0"} {
		t.Run(index, func(t *testing.T) {
			t.Parallel()
			f := workbook(t, [][]string{{"Category", "Skill", "Junior"}, {"Go", "Channels", "send"}})
			malformed := rewriteWorksheetXML(t, encoded(t, f), func(xml string) string { return strings.Replace(xml, "<v>5</v>", "<v>"+index+"</v>", 1) })
			if _, err := adapter.Preview(bytes.NewReader(malformed), "", adapter.Mapping{}); err == nil {
				t.Fatal("invalid shared-string index accepted")
			}
			if _, err := adapter.Sheets(bytes.NewReader(malformed)); err == nil {
				t.Fatal("sheet listing bypassed shared-string validation")
			}
		})
	}
}

func TestSharedStringValidationRejectsDuplicateNormalizedParts(t *testing.T) {
	t.Parallel()
	f := workbook(t, [][]string{{"Category", "Skill", "Junior"}, {"Go", "Channels", "send"}})
	malformed := addZIPEntry(t, encoded(t, f), `XL\sharedStrings.xml`, []byte(`<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"></sst>`))
	if _, err := adapter.Sheets(bytes.NewReader(malformed)); err == nil {
		t.Fatal("duplicate normalized shared-string table accepted")
	}
}

func TestSharedStringValidationInspectsXMLRegardlessOfFilename(t *testing.T) {
	t.Parallel()
	f := workbook(t, [][]string{{"Category", "Skill", "Junior"}, {"Go", "Channels", "send"}})
	malformed := addZIPEntry(t, encoded(t, f), "xl/custom-part.bin", []byte(`<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="s"><v>-1</v></c></row></sheetData></worksheet>`))
	if _, err := adapter.Sheets(bytes.NewReader(malformed)); err == nil {
		t.Fatal("XML part with nonstandard filename bypassed shared-string validation")
	}
}
