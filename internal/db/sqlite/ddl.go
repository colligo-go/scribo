package sqlite

import (
	"strings"

	"github.com/colligo-go/scribo/internal/db"
)

func CreateTable(definition db.TableDefinition) string {
	var out strings.Builder
	out.WriteString("CREATE TABLE " + quoteIdent(definition.Name) + " (\n")
	for _, column := range definition.Columns {
		out.WriteString("  " + quoteIdent(column.Name) + " " + column.Type)
		if column.NotNull {
			out.WriteString(" NOT NULL")
		}
		out.WriteString(",\n")
	}
	unique := make([]string, len(definition.Unique))
	for i, name := range definition.Unique {
		unique[i] = quoteIdent(name)
	}
	out.WriteString("  UNIQUE (" + strings.Join(unique, ", ") + ")\n")
	out.WriteString(") STRICT;\n")
	return out.String()
}
