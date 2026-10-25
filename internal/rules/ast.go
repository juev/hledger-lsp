package rules

import "github.com/juev/hledger-lsp/internal/ast"

// RulesFile is the top-level AST node for a parsed .rules file.
type RulesFile struct {
	Directives  []SimpleDirective
	FieldsDefs  []FieldsDirective
	Assignments []FieldAssignment
	IfBlocks    []IfBlock
	IfTables    []IfTable
	Includes    []IncludeDirective
	Sources     []SourceDirective
	Comments    []RulesComment
}

// SimpleDirective represents most single-line directives.
type SimpleDirective struct {
	Name  string
	Value string
	Range ast.Range
}

// FieldsDirective represents the "fields" directive with its column names.
type FieldsDirective struct {
	Names []string
	Range ast.Range
}

// FieldAssignment represents "fieldname value" pairs.
type FieldAssignment struct {
	Field string
	Value string
	Range ast.Range
}

// IfBlock represents an "if" conditional block.
type IfBlock struct {
	Patterns    []string
	Assignments []FieldAssignment
	Range       ast.Range
}

// IfTable represents an "if" table ("if,account2,comment" followed by rows).
// Each row is equivalent to an if block, and is also listed in RulesFile.IfBlocks.
type IfTable struct {
	Fields []string
	Rows   []IfBlock
	Range  ast.Range
}

// IncludeDirective represents an "include path" directive.
type IncludeDirective struct {
	Path  string
	Range ast.Range
}

// SourceDirective represents a "source path" directive.
type SourceDirective struct {
	Path  string
	Range ast.Range
}

// RulesComment represents a comment line.
type RulesComment struct {
	Text  string
	Range ast.Range
}
