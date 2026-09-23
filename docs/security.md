# Dependency findings and XLSX input validation

As of 2026-09-23, `govulncheck` reports [GO-2026-6452 / CVE-2026-59162](https://pkg.go.dev/vuln/GO-2026-6452) for the pinned `github.com/xuri/excelize/v2 v2.11.0`. The Go vulnerability database currently describes all versions as affected and lists no known fixed version. Do not describe the dependency scan as clean or suppress this finding.

The advisory describes a negative shared-string index causing a runtime panic during cell lookup. The installed v2.11.0 module source inspected during implementation already contains a lower and upper bounds check in `xlsxC.getValueFrom` (`cell.go`, around line 626). Consequently the test environment did **not** reproduce the reported panic. It did reproduce another unacceptable behavior at the same input boundary: `Rows.Columns` discarded the invalid cell lookup error, and a negative shared-string index in a requirement cell became an empty requirement accepted by Preview.

The service now validates XLSX shared-string references before calling Excelize on uploaded data:

- ZIP payloads retain the 10 MiB compressed and inflated limits. Individual entries are also read through a bounded reader.
- Duplicate archive part names after slash and case normalization are rejected, preventing two different shared-string tables from being interpreted inconsistently.
- The actual number of direct `si` entries in the shared-string table is counted; claimed `count` attributes are not trusted.
- Every XML payload is inspected, including worksheet XML placed under nonstandard filename extensions. Each `c` cell with `t="s"` must have a numeric, nonnegative index strictly below the actual shared-string count. Missing, overflowed, negative, fractional and out-of-range indices are rejected.
- Invalid XML and multiple XML root elements are rejected by inspection. Formulas and macro payloads remain rejected by the existing import rules.

This validation runs in both `Sheets` and `Preview`, including the HTTP import preview/confirm path. The separate Excelize reader used to add supplemental export sheets consumes only a freshly generated workbook; it does not accept uploaded bytes. Output values are written explicitly as strings.

`TestPreviewRejectsNegativeSharedStringIndexWithoutPanic` contains a recovery guard so a future panic becomes a regression failure. Before the boundary change it failed with `negative shared-string index accepted`; afterward it passes. Additional tests cover out-of-range values, invalid numeric representations, integer overflow, duplicate normalized shared-string parts, and XML under a `.bin` filename. Ordinary imports, round trips and literal formula-looking strings remain covered by the adapter suite.

The boundary mitigates this service's reachable malformed-index path. It is not a claim that Excelize is free from other vulnerabilities, or that the advisory database has been corrected. Continue running `govulncheck` and review the reported finding. When upstream release/advisory metadata identifies a fixed supported version, update the dependency and retain these regression tests. The upstream reference linked by the advisory is [commit 93f0b3c](https://github.com/qax-os/excelize/commit/93f0b3caed37f21ef5079e3259c6c21dcfe68453).
