package particle

import (
	"errors"
	"fmt"
	"github.com/dave/jennifer/jen"
	"go/ast"
	"reflect"
	"strings"
)

type GeneratorOpts struct {
	// Package is the name of the package to generate the code in.
	Package string `json:"package,omitempty"`

	// StructTag is the name of the struct tag to use for accessing the value
	// of the field in the partial type's helper methods.
	StructTag string `json:"structTag,omitempty"`

	// TypePrefix is the prefix to use for the generated partial types.
	TypePrefix string `json:"typePrefix,omitempty"`
}

type Generator struct {
	// opts are the generator options.
	opts GeneratorOpts

	// file is the jen.File to write the generated code to.
	file *jen.File
}

// NewGenerator creates a new generator configured by the given GeneratorOpts.
func NewGenerator(opts GeneratorOpts) *Generator {
	trg := jen.NewFile(opts.Package)
	trg.HeaderComment("Code generated by particle.")
	trg.HeaderComment("https://github.com/lukasl-dev/particle")

	return &Generator{opts: opts, file: trg}
}

// Generate generates the code for the current file.
func (g *Generator) Generate() string {
	return g.file.GoString()
}

// File generates the code for the entire file.
func (g *Generator) File(file *ast.File) {
	g.Imports(file.Imports)
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.TypeSpec:
			st, isStruct := node.Type.(*ast.StructType)
			if !isStruct {
				return true
			}
			g.Type(node)
			for _, field := range st.Fields.List {
				g.AccessFunc(node, field, file.Imports)
				g.Line()
				g.WithFunc(node, field, file.Imports)
				g.Line()
			}
		}
		return true
	})
}

// Imports generates the code for the given import specs.
func (g *Generator) Imports(imp []*ast.ImportSpec) {
	for _, i := range imp {
		g.Import(i)
	}
}

// Import generates the code for the given import spec.
func (g *Generator) Import(imp *ast.ImportSpec) {
	if imp.Name == nil {
		g.file.ImportName(strings.Trim(imp.Path.Value, "\""), "")
		return
	}
	g.file.ImportAlias(strings.Trim(imp.Path.Value, "\""), imp.Name.Name)
}

// Type generates a "partial" map-type.
func (g *Generator) Type(typ *ast.TypeSpec) {
	_, isStruct := typ.Type.(*ast.StructType)
	if !isStruct {
		panic("partial type must be a struct")
	}

	g.file.Commentf(
		"%s%s is a partial type.",
		g.opts.TypePrefix,
		typ.Name.Name,
	)
	g.file.Type().Id(g.opts.TypePrefix + typ.Name.Name).Map(jen.String()).Any()
}

// AccessFunc generates the code for the function used to access a field.
func (g *Generator) AccessFunc(typ *ast.TypeSpec, field *ast.Field, imp []*ast.ImportSpec) {
	if len(field.Names) == 0 {
		panic("field must have a name")
	}

	fieldName, fieldKey := g.fieldNames(field)

	g.file.Commentf(
		"%s returns the value of the '%s' field.",
		fieldName,
		fieldKey,
	)
	g.file.Func().
		Params(jen.Id("p").Id(g.opts.TypePrefix + typ.Name.Name)).
		Id(fieldName).
		Params().
		Add(g.determineType(field.Type, imp)).
		BlockFunc(func(bg *jen.Group) {
			bg.Return(jen.Id("p").
				Index(jen.Lit(fieldKey))).
				Assert(g.determineType(field.Type, imp))
		})
}

// WithFunc generates the code for the function used to update a field.
func (g *Generator) WithFunc(typ *ast.TypeSpec, field *ast.Field, imp []*ast.ImportSpec) {
	fieldName, fieldKey := g.fieldNames(field)

	g.file.Commentf(
		"With%s updates p with the given v and returns p again.",
		fieldName,
	)
	g.file.Func().
		Params(jen.Id("p").Id(g.opts.TypePrefix + typ.Name.Name)).
		Id("With" + fieldName).
		ParamsFunc(func(pg *jen.Group) {
			pg.Id("v").Add(g.determineType(field.Type, imp))
		}).
		Id(g.opts.TypePrefix + typ.Name.Name).
		BlockFunc(func(fg *jen.Group) {
			fg.Id("p").
				Index(jen.Lit(fieldKey)).
				Op("=").
				Id("v")
			fg.Return(jen.Id("p"))
		})
}

// Line inserts an empty line into the generated code.
func (g *Generator) Line() {
	g.file.Line()
}

// fieldNames returns the name of the field and the respective key to use in
// the map. The key is either the field name or the value of the struct tag
// with the name specified in the generator options.
func (g *Generator) fieldNames(field *ast.Field) (fieldName, fieldKey string) {
	if g.opts.StructTag != "" && field.Tag != nil {
		tag := reflect.StructTag(field.Tag.Value).Get(g.opts.StructTag)
		if tag != "" {
			i := strings.Index(tag, ",")
			if i != -1 {
				tag = tag[:i]
			}
			return field.Names[0].Name, tag
		}
	}
	return field.Names[0].Name, field.Names[0].Name
}

func (g *Generator) determineType(typ ast.Expr, imp []*ast.ImportSpec) jen.Code {
	switch x := typ.(type) {
	case *ast.Ident:
		return jen.Id(x.Name)

	case *ast.SelectorExpr:
		fieldTypeName, fieldTypePkg, err := g.qualify(x, imp)
		if err != nil {
			panic("could not qualify type: " + err.Error())
		}
		return jen.Qual(fieldTypePkg, fieldTypeName)

	case *ast.MapType:
		key := g.determineType(x.Key, imp)
		value := g.determineType(x.Value, imp)
		return jen.Map(key).Add(value)

	case *ast.ArrayType:
		return jen.Index().Add(g.determineType(x.Elt, imp))

	case *ast.StarExpr:
		return jen.Op("*").Add(g.determineType(x.X, imp))

	default:
		panic(fmt.Sprintf("unsupported type %T", x))
	}
}

// qualify resolves the type of the given expression and returns the name of
// the type and the package it is defined in.
//
// Example: Given the import "github.com/lukasl-dev/particle" and the type
// "particle.Generator", the function returns "Generator" and
// "github.com/lukasl-dev/particle".
func (g *Generator) qualify(
	expr *ast.SelectorExpr,
	imports []*ast.ImportSpec,
) (name, pkg string, err error) {
	pkgName := expr.X.(*ast.Ident).Name

	for _, imp := range imports {
		path := strings.Trim(imp.Path.Value, "\"")

		split := strings.Split(path, "/")
		last := split[len(split)-1]

		if last == pkgName || (imp.Name != nil && imp.Name.Name == pkgName) {
			return expr.Sel.Name, path, nil
		}
	}

	return "", "", errors.New("could not find import")
}
