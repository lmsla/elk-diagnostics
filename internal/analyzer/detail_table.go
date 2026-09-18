package analyzer

import "elk-diagnostics/internal/diagnostic"

func newDetailTable(title, note string, columns ...string) *diagnostic.DetailTable {
	return &diagnostic.DetailTable{
		Title:        title,
		Columns:      columns,
		StatusColumn: true,
		Note:         note,
	}
}

func addDetailRow(table *diagnostic.DetailTable, status diagnostic.Status, values ...string) {
	table.Rows = append(table.Rows, diagnostic.DetailTableRow{Values: values, Status: status})
}
