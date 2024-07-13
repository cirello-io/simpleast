// Package simpleast provides a simple way to inspect Go files and extract
// structs and methods from them.
package simpleast

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"slices"
)

// Struct represents a Go struct. Fields and Methods are not ordered.
type Struct struct {
	Name       string   `json:"name,omitempty"`
	DocComment string   `json:"doc_comment,omitempty"`
	Fields     []Field  `json:"fields,omitempty"`
	Methods    []Method `json:"methods,omitempty"`
}

// Field represents a Go field of Struct.
type Field struct {
	Name       string `json:"name,omitempty"`
	DocComment string `json:"doc_comment,omitempty"`
	Type       string `json:"type,omitempty"`
	Tags       string `json:"tags,omitempty"`
}

// Method represents a Go method of Struct.
type Method struct {
	Name       string  `json:"name,omitempty"`
	DocComment string  `json:"doc_comment,omitempty"`
	Parameters []Field `json:"parameters,omitempty"`
	Results    []Field `json:"results,omitempty"`

	structName string
}

// ParseStructs extracts structs and methods from a Go file.
func ParseStructs(r io.Reader) ([]*Struct, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.AllErrors|parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}
	var structs []*Struct
	ast.Inspect(f, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.GenDecl:
			structs = append(structs, parseASTSpecs(decl.Specs)...)
		case *ast.FuncDecl:
			receivers := parseASTFuncDecl(decl)
			for _, receiver := range receivers {
				idx := slices.IndexFunc(structs, func(s *Struct) bool { return s.Name == receiver.structName })
				if idx == -1 {
					continue
				}
				structs[idx].Methods = append(structs[idx].Methods, receiver)
			}
		}
		return true
	})
	return structs, nil
}

func expressionString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + expressionString(e.X)
	case *ast.SelectorExpr:
		return expressionString(e.X) + "." + e.Sel.Name
	case *ast.ArrayType:
		return "[]" + expressionString(e.Elt)
	case *ast.MapType:
		return "map[" + expressionString(e.Key) + "]" + expressionString(e.Value)
	default:
		return ""
	}
}

func parseASTSpecs(specs []ast.Spec) []*Struct {
	structs := []*Struct{}
	for _, spec := range specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			continue
		}
		fields := []Field{}
		for _, field := range structType.Fields.List {
			fieldTags := ""
			if field.Tag != nil {
				fieldTags = field.Tag.Value
			}
			fieldComment := field.Doc.Text()
			for _, fieldName := range field.Names {
				fields = append(fields, Field{
					Name:       fieldName.Name,
					DocComment: fieldComment,
					Type:       expressionString(field.Type),
					Tags:       fieldTags,
				})
			}
		}
		structs = append(structs, &Struct{
			Name:       typeSpec.Name.Name,
			DocComment: typeSpec.Doc.Text(),
			Fields:     fields,
			Methods:    []Method{},
		})
	}
	return structs
}

func parseASTFuncDecl(decl *ast.FuncDecl) []Method {
	methods := []Method{}
	if decl.Recv == nil {
		return methods
	}
	for _, recv := range decl.Recv.List {
		structName := ""
		switch t := recv.Type.(type) {
		case *ast.StarExpr:
			if ident, ok := t.X.(*ast.Ident); ok {
				structName = ident.Name
			}
		case *ast.Ident:
			structName = t.Name
		}
		if structName == "" {
			continue
		}
		params := []Field{}
		for _, param := range decl.Type.Params.List {
			for _, paramName := range param.Names {
				params = append(params, Field{
					Name: paramName.Name,
					Type: expressionString(param.Type),
				})
			}
		}
		returns := []Field{}
		if decl.Type.Results != nil {
			for _, result := range decl.Type.Results.List {
				returns = append(returns, Field{
					Type: expressionString(result.Type),
				})
			}
		}
		methods = append(methods, Method{
			structName: structName,
			Name:       decl.Name.Name,
			DocComment: decl.Doc.Text(),
			Parameters: params,
			Results:    returns,
		})
	}
	return methods
}
