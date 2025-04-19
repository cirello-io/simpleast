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
	"strconv"
	"strings"
)

// Struct represents a Go struct. Fields and Methods are not ordered.
type Struct struct {
	Name       string   `json:"name,omitempty"`
	DocComment string   `json:"doc_comment,omitempty"`
	TypeParams []Field  `json:"type_params,omitempty"`
	Fields     []Field  `json:"fields,omitempty"`
	Methods    []Method `json:"methods,omitempty"`
	Alias      string   `json:"alias,omitempty"`
	Type       string   `json:"type,omitempty"`
}

// Field represents a Go field of Struct.
type Field struct {
	Name       string     `json:"name,omitempty"`
	DocComment string     `json:"doc_comment,omitempty"`
	Type       string     `json:"type,omitempty"`
	Tags       StructTags `json:"tags,omitempty"`
}

type StructTags []StructTag

func (st StructTags) Get(name string) string {
	for _, tag := range st {
		if tag.Name == name {
			return tag.Value
		}
	}
	return ""
}

// StructTag represents a Go struct field tag.
type StructTag struct {
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

// Method represents a Go method of Struct.
type Method struct {
	Name       string   `json:"name,omitempty"`
	DocComment string   `json:"doc_comment,omitempty"`
	TypeParams []string `json:"type_params,omitempty"`
	Parameters []Field  `json:"parameters,omitempty"`
	Results    []Field  `json:"results,omitempty"`

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
			isSingleTypeGroup := len(decl.Specs) == 1
			hasOneDocGroup := decl.Doc != nil && len(decl.Doc.List) != 0
			if isSingleTypeGroup && hasOneDocGroup {
				typeSpec, isTypeSpec := decl.Specs[0].(*ast.TypeSpec)
				if isTypeSpec && typeSpec.Doc == nil {
					typeSpec.Doc = decl.Doc
				}
			}
			structs = append(structs, parseASTSpecs(decl.Specs)...)
		case *ast.FuncDecl:
			receivers := parseASTFuncDecl(decl)
			for _, receiver := range receivers {
				idx := slices.IndexFunc(structs, func(s *Struct) bool { return s.Name == receiver.structName })
				if idx == -1 {
					structs = append(structs, &Struct{
						Name:    receiver.structName,
						Methods: []Method{receiver},
					})
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
	case *ast.BasicLit:
		v, err := strconv.Unquote(e.Value)
		if err != nil {
			return e.Value
		}
		return v
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
		switch v := typeSpec.Type.(type) {
		case *ast.StructType:
			structType := v
			fields := []Field{}
			for _, field := range structType.Fields.List {
				fieldTags := ""
				if field.Tag != nil {
					fieldTags = field.Tag.Value
				}
				fieldComment := field.Doc.Text()
				if len(field.Names) > 0 {
					for _, fieldName := range field.Names {
						fields = append(fields, Field{
							Name:       fieldName.Name,
							DocComment: fieldComment,
							Type:       expressionString(field.Type),
							Tags:       parseFieldTags(fieldTags),
						})
					}
				} else {
					fields = append(fields, Field{
						Name:       "",
						DocComment: fieldComment,
						Type:       expressionString(field.Type),
						Tags:       parseFieldTags(fieldTags),
					})
				}
			}
			typeParams := []Field{}
			if typeSpec.TypeParams != nil {
				for _, field := range typeSpec.TypeParams.List {
					for _, name := range field.Names {
						typeParams = append(typeParams, Field{
							Name: name.Name,
							Type: expressionString(field.Type),
						})
					}
				}
			}
			structs = append(structs, &Struct{
				Name:       typeSpec.Name.Name,
				DocComment: typeSpec.Doc.Text(),
				TypeParams: typeParams,
				Fields:     fields,
				Methods:    []Method{},
			})
		case *ast.Ident:
			if typeSpec.Assign.IsValid() {
				structs = append(structs, &Struct{
					Name:       typeSpec.Name.Name,
					DocComment: typeSpec.Doc.Text(),
					Alias:      v.Name,
				})
			} else {
				structs = append(structs, &Struct{
					Name:       typeSpec.Name.Name,
					DocComment: typeSpec.Doc.Text(),
					Type:       v.Name,
				})
			}
		case *ast.ArrayType:
			structs = append(structs, &Struct{
				Name:       typeSpec.Name.Name,
				DocComment: typeSpec.Doc.Text(),
				Type:       "[]" + v.Elt.(*ast.Ident).Name,
			})
		}
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
		typeParamsExpr := []ast.Expr{}
		switch t := recv.Type.(type) {
		case *ast.StarExpr:
			if ident, ok := t.X.(*ast.Ident); ok {
				structName = ident.Name
			} else if indexList, ok := t.X.(*ast.IndexListExpr); ok {
				if ident, ok := indexList.X.(*ast.Ident); ok {
					structName = ident.Name
				}
				typeParamsExpr = indexList.Indices
			} else if index, ok := t.X.(*ast.IndexExpr); ok {
				if ident, ok := index.X.(*ast.Ident); ok {
					structName = ident.Name
				}
				typeParamsExpr = []ast.Expr{index.Index}
			}

		case *ast.Ident:
			structName = t.Name
		case *ast.IndexListExpr:
			if ident, ok := t.X.(*ast.Ident); ok {
				structName = ident.Name
			}
			typeParamsExpr = t.Indices
		case *ast.IndexExpr:
			if ident, ok := t.X.(*ast.Ident); ok {
				structName = ident.Name
			}
			typeParamsExpr = []ast.Expr{t.Index}
		}
		typeParams := []string{}
		for _, index := range typeParamsExpr {
			if ident, ok := index.(*ast.Ident); ok {
				typeParams = append(typeParams, ident.Name)
			}
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
			TypeParams: typeParams,
			Parameters: params,
			Results:    returns,
		})
	}
	return methods
}

func parseFieldTags(tags string) StructTags {
	// A StructTag is the tag string in a struct field.
	//
	// By convention, tag strings are a concatenation of
	// optionally space-separated key:"value" pairs.
	// Each key is a non-empty string consisting of non-control
	// characters other than space (U+0020 ' '), quote (U+0022 '"'),
	// and colon (U+003A ':').  Each value is quoted using U+0022 '"'
	// characters and Go string literal syntax.

	tags = strings.Trim(tags, "`")
	tags = strings.TrimLeft(tags, " ")
	var structTags StructTags
	currentTag := StructTag{}
	for tags != "" {
		colonDivider := strings.Index(tags, ":")
		if colonDivider == -1 {
			break
		}
		name := tags[:colonDivider]
		currentTag.Name = name
		tags = tags[colonDivider+1:]
		insideQuotes := false
		for i := 0; i < len(tags); i++ {
			if !insideQuotes && tags[i] == '"' {
				insideQuotes = true
			} else if insideQuotes && tags[i] == '\\' {
				i++
			} else if insideQuotes && tags[i] == '"' {
				v, err := strconv.Unquote(tags[:i+1])
				if err != nil {
					break
				}
				currentTag.Value = v
				tags = strings.TrimLeft(tags[i+1:], " ")
				structTags = append(structTags, currentTag)
				currentTag = StructTag{}
			}
		}
	}
	return structTags
}

type Const struct {
	Name       string `json:"name,omitempty"`
	DocComment string `json:"doc_comment,omitempty"`
	Type       string `json:"type,omitempty"`
	Value      string `json:"value,omitempty"`
}

func ParseConsts(r io.Reader) ([]*Const, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.AllErrors|parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}
	var constants []*Const
	ast.Inspect(f, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.GenDecl:
			if decl.Tok != token.CONST {
				return true
			}
			for _, spec := range decl.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				docComment := valueSpec.Doc.Text()
				for i, name := range valueSpec.Names {
					if docComment == "" {
						docComment = decl.Doc.Text()
					}
					constants = append(constants, &Const{
						Name:       name.Name,
						DocComment: docComment,
						Type:       expressionString(valueSpec.Type),
						Value:      expressionString(valueSpec.Values[i]),
					})
				}
			}
		}
		return true
	})
	return constants, nil
}

func ExtractPackageName(r io.Reader) (string, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.AllErrors|parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parse file: %w", err)
	}
	var pkgName string
	ast.Inspect(f, func(n ast.Node) bool {
		if pkg, ok := n.(*ast.File); ok {
			pkgName = pkg.Name.String()
			return false
		}
		return true
	})
	return pkgName, nil
}
