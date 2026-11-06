package contract

import (
	"os"
	"strings"
	"testing"
)

func TestToolRunsDDLMatchesContractDocument(t *testing.T) {
	doc, err := os.ReadFile("../../docs/CONTRACT.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), ToolRunsDDL) {
		t.Error("ToolRunsDDL does not appear verbatim in docs/CONTRACT.md")
	}
}
