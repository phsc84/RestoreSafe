package view

// Table is a list of like items shown as a table whose columns the user
// can widen (GUI spec 3.4): folders, sets, destinations. Statements and single
// facts are lines, not tables.
type Table struct {
	// Name is what screen readers announce for the table.
	Name    string
	Columns []Column
	Rows    []TableRow
}

// Column is a column of a table.
type Column struct {
	Title string
	// Width is the column's starting width in DIPs; Fill columns share
	// the width the others leave.
	Width int32
	Fill  bool
	Right bool
}

// TableRow is a row: one cell per column.
type TableRow struct {
	Cells []TableCell
	// Tip is shown when the mouse rests on the row: what a cell may not
	// show in full, such as the path.
	Tip string
}

// TableCell is the content of a cell: text in a tone, or a type badge.
type TableCell struct {
	Text  string
	Tone  Tone
	Badge *Badge
}

// Signature identifies the table's columns: a table whose signature
// changed gets new columns.
func (t Table) Signature() string {
	s := t.Name
	for _, c := range t.Columns {
		s += "\x00" + c.Title
	}
	return s
}

// joinTip joins the non-empty lines of a row's tooltip.
func joinTip(lines ...string) string {
	out := ""
	for _, l := range lines {
		if l == "" {
			continue
		}
		if out != "" {
			out += "\n"
		}
		out += l
	}
	return out
}
