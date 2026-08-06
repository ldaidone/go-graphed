package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

func parseSQL(t *testing.T, name, src string) ir.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	file := scanner.File{Path: path, Language: "sql", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	return doc
}

func TestParse_SQLFile_EmptyFile(t *testing.T) {
	doc := parseSQL(t, "empty.sql", "")
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty SQL file, got %d", len(doc.Entities))
	}
	if doc.Metadata["processor"] != "sql-extractor" {
		t.Errorf("processor = %q, want sql-extractor", doc.Metadata["processor"])
	}
}

func TestParse_SQLFile_Objects(t *testing.T) {
	src := `CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    email TEXT UNIQUE
);

CREATE INDEX idx_users_email ON users(email);

CREATE VIEW active_users AS
SELECT * FROM users;

CREATE FUNCTION total_users() RETURNS INTEGER AS $$
BEGIN
    RETURN 1;
END;
$$ LANGUAGE plpgsql;
`
	doc := parseSQL(t, "objects.sql", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	if byName["users"] != "table" {
		t.Errorf("users type = %q, want table", byName["users"])
	}
	if byName["idx_users_email"] != "index" {
		t.Errorf("idx_users_email type = %q, want index", byName["idx_users_email"])
	}
	if byName["active_users"] != "view" {
		t.Errorf("active_users type = %q, want view", byName["active_users"])
	}
	if byName["total_users"] != "function" {
		t.Errorf("total_users type = %q, want function", byName["total_users"])
	}
}

func TestParse_SQLFile_ViewReferencesTables(t *testing.T) {
	src := `CREATE TABLE users (id INTEGER);
CREATE TABLE orders (
    id INTEGER,
    user_id INTEGER REFERENCES users(id)
);

CREATE VIEW active AS
SELECT u.email, o.total
FROM users u
JOIN orders o ON o.user_id = u.id;
`
	doc := parseSQL(t, "refs.sql", src)

	names := nameByID(doc)
	for _, l := range doc.Links {
		if l.Type != "references" {
			t.Errorf("unexpected link type %q: %+v", l.Type, l)
		}
	}

	srcName := func(l ir.Link) (string, string) {
		return names[l.SourceID], names[l.TargetID]
	}

	var fk, viewUsers, viewOrders bool
	for _, l := range doc.Links {
		s, trg := srcName(l)
		switch {
		case s == "orders" && trg == "users":
			fk = true
		case s == "active" && trg == "users":
			viewUsers = true
		case s == "active" && trg == "orders":
			viewOrders = true
		}
		if l.SourceType != ir.LinkSourceExtracted {
			t.Errorf("references link provenance = %q, want %q", l.SourceType, ir.LinkSourceExtracted)
		}
	}
	if !fk {
		t.Errorf("expected orders -> users foreign-key reference")
	}
	if !viewUsers {
		t.Errorf("expected active -> users reference")
	}
	if !viewOrders {
		t.Errorf("expected active -> orders reference")
	}

	// The view's own name and column identifiers must not resolve.
	for _, l := range doc.Links {
		if names[l.TargetID] == "active" || names[l.SourceID] == "email" {
			t.Errorf("spurious reference: %+v", l)
		}
	}
}

func TestParse_SQLFile_NoReferencesToUnknownTables(t *testing.T) {
	src := `CREATE TABLE users (id INTEGER);

CREATE VIEW broken AS
SELECT * FROM missing_table;
`
	doc := parseSQL(t, "unknown.sql", src)
	for _, l := range doc.Links {
		if strings.Contains(l.TargetID, "missing_table") {
			t.Errorf("references to unindexed tables must not resolve, got %+v", l)
		}
	}
}

func TestParse_SQLFile_SyntacticallyInvalid(t *testing.T) {
	src := `CREATE TABLE users (
    id INTEGER
`
	doc := parseSQL(t, "invalid.sql", src)
	for _, e := range doc.Entities {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entity has empty ID or Name: %+v", e)
		}
	}
}
