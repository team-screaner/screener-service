# XLSX interchange

`internal/adapter/xlsx` is a stateless adapter using pinned `github.com/xuri/excelize/v2 v2.11.0`. It does not access storage or the network. Types use snake_case JSON tags.

```go
Sheets(reader io.Reader) ([]string, error)
Preview(reader io.Reader, sheet string, mapping Mapping) (Document, error)
Export(document Document) ([]byte, error)
```

`Mapping` supplies `category`, `skill`, and an ordered `levels` array of **header names**, not Excel column letters. Empty category and skill mappings default to `Category` and `Skill`. An empty levels array detects all remaining headers in worksheet order. Explicit levels select and order a subset. Names match case sensitively after trimming surrounding whitespace. Header names must be unique and nonempty; mapped columns must exist and mappings must not overlap.

`Document` contains `name`, ordered `levels`, and `rows`. Each row contains `category`, `skill`, and a `requirements` object keyed by level name. Preview defaults to the first sheet when `sheet` is empty; its document name is the selected sheet. Category and skill values are trimmed and required. Their pair must be unique; the same skill may appear in different categories. Empty requirement values are valid and are retained. Completely blank rows are skipped; physical rows still count toward the limit. Row 1 is always the header. Merged category cells are not forward-filled: every skill row needs its category.

Export writes a single worksheet named after the document (default `Matrix`). Names that violate Excel's sheet naming rules (including more than 31 characters or `[]:*?/\`) fall back to `Matrix`. This means such titles are not preserved by a file round trip; the application retains the original matrix title in storage. Every cell is explicitly written as a string, preserving formula-looking literals such as `=1+1`, `+...`, and `@...`. Export validates row uniqueness, required values, levels and limits before writing, and checks archive size afterward. Requirement keys not present in document levels are rejected.

Limits apply before costly worksheet materialization:

- Compressed upload and total uncompressed ZIP payload: 10 MiB each.
- A cell: 32,767 UTF-8 bytes.
- A sheet: 2,000 physical data rows plus one header; 22 physical columns.
- Selected levels: 1–20.
- Export's aggregate input text: 10 MiB, including repeated strings; final uncompressed workbook must also fit 10 MiB.

Macro payloads and Excel 4 macro sheets are rejected. Preview rejects formulas anywhere in the selected sheet's used cells, including headers, ignored columns and rows with no cached formula result. The adapter never calculates formulas. Other worksheets' formulas are not inspected during sheet listing or preview of an unrelated sheet. No source workbook content, links, metadata or macros are copied into exports.

Upload reads use `io.LimitReader`; ZIP sizes are checked without inflation, and Excelize also receives explicit unzip limits. Worksheet rows are streamed and bounded before formula inspection. Workbook and iterator close errors are propagated. Caller owns the input reader's lifetime.

## TDD evidence

Executed locally on 2026-09-23 with Go 1.27.1. Tests are black-box `package xlsx_test` and operate on real in-memory XLSX archives.

1. Added round-trip/sheet-list test before implementation: failed with `no non-test Go files`; implemented adapter; passed.
2. Added mapping and invalid-data scenarios: 12 subtests failed (`invalid workbook accepted`); implemented header, mapping and row validation; passed.
3. Added malicious formulas and limits: formula cells A2/B2/C2/C3 and cell/rows/levels/uncompressed/macros cases failed; implemented bounded reading, macro rejection and formula inspection; passed. Literal formula export tests passed using `SetCellStr`.
4. Added export aggregate-limit regression: failed with `invalid document exported`; implemented aggregate budget and final archive budget; passed.
5. Added aggregate category text with absent requirements: failed with `invalid document exported`; moved budget check to cover identity cells as well; passed.

6. Added invalid and long worksheet names: three cases failed with Excel sheet name errors; implemented the `Matrix` fallback; passed.

Validation commands:

```sh
go test -race -shuffle=on ./internal/adapter/xlsx
go vet ./internal/adapter/xlsx
golangci-lint run ./internal/adapter/xlsx/...
```

Race/shuffled tests and vet pass. Golangci-lint reports `0 issues.` No containers or live applications are needed for this pure file adapter.

Official library reference: [Excelize workbook options and OpenReader](https://xuri.me/excelize/en/workbook.html).
