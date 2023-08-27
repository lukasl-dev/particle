package main

import (
	"errors"
	"fmt"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/lukasl-dev/particle"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type rootCmd struct {
	// dryRun is a flag that indicates whether the command should be run in dry
	// run mode.
	dryRun bool

	// globs is a slice of ("doublestar") glob patterns to match files against.
	globs []string

	// dir is the working directory to use. Defaults to ".".
	dir string

	// out is the output file or directory to write the generated code to.
	out string

	// structTag is the name of the struct tag to use for indexing the partial
	// map. Defaults to "particle".
	structTag string

	// typePrefix is the prefix to use for the generated partial types.
	typePrefix string

	// pkg is the name of the package to generate the code in.
	pkg string
}

// root creates the root cobra.Command and returns it.
func root() *cobra.Command {
	return new(rootCmd).build()
}

// bind binds the command flags to c.
func (c *rootCmd) bind(fs *pflag.FlagSet) {
	fs.BoolVar(
		&c.dryRun,
		"dry-run",
		false,
		"Whether to run the command in dry run mode.",
	)
	fs.StringSliceVarP(
		&c.globs,
		"glob",
		"g",
		nil,
		"The glob patterns to match files against.",
	)
	fs.StringVarP(
		&c.dir,
		"dir",
		"d",
		".",
		"The working directory to use.",
	)
	fs.StringVarP(
		&c.out,
		"out",
		"o",
		"partial",
		"The output file or directory to write the generated code to.",
	)
	fs.StringVar(
		&c.structTag,
		"struct-tag",
		"particle",
		"The struct tag to use for indexing the partial map.",
	)
	fs.StringVar(
		&c.typePrefix,
		"type-prefix",
		"",
		"The prefix to use for the generated partial types.",
	)
	fs.StringVar(
		&c.pkg,
		"package",
		"partial",
		"The name of the package to generate the code in.",
	)
}

// build builds the root cobra.Command and returns it.
func (c *rootCmd) build() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "particle",
		Short:   "A generator for partial struct",
		PreRunE: c.pre,
		RunE:    c.run,
	}
	c.bind(cmd.Flags())
	return cmd
}

// pre validates c's flags.
func (c *rootCmd) pre(*cobra.Command, []string) error {
	switch {
	case len(c.globs) == 0:
		return errors.New("no glob patterns given: use --glob <pattern> to specify glob patterns")
	case !c.dryRun && c.out == "":
		return errors.New("no output file or directory given: use --out <path> to specify an output file or directory")
	default:
		return nil
	}
}

// run runs the command.
func (c *rootCmd) run(*cobra.Command, []string) error {
	paths, err := c.glob()
	if err != nil {
		return fmt.Errorf("could not glob: %w", err)
	}

	for i, srcPath := range paths {
		code, err := c.generate(srcPath)
		if err != nil {
			return fmt.Errorf("could not generate code: %w", err)
		}

		if c.dryRun {
			fmt.Println("// Source:", srcPath)
			fmt.Println(code)
			if i != len(paths)-1 {
				fmt.Println("---")
			}
			continue
		}

		if err := c.writeInto(srcPath, code); err != nil {
			return fmt.Errorf("could not write: %w", err)
		}
	}

	return nil
}

// glob returns the paths that match the globs.
func (c *rootCmd) glob() ([]string, error) {
	return doublestar.Glob(
		os.DirFS(c.dir),
		fmt.Sprintf("{%s}", strings.Join(c.globs, ",")),
	)
}

// generate generates the code for the given file path.
func (c *rootCmd) generate(path string) (string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("could not parse file: %w", err)
	}

	opts := particle.GeneratorOpts{
		Package:    c.pkg,
		StructTag:  c.structTag,
		TypePrefix: c.typePrefix,
	}
	g := particle.NewGenerator(opts)
	g.File(file)
	return g.Generate(), nil
}

// writeInto writes the given code into the output file or directory.
func (c *rootCmd) writeInto(srcPath, code string) error {
	if strings.HasSuffix(c.out, ".go") {
		_, err := os.Stat(c.out)
		if os.IsNotExist(err) {
			if err := os.WriteFile(c.out, []byte(code), 0644); err != nil {
				return fmt.Errorf("could not write file: %w", err)
			}
		}
		return nil
	}

	if err := os.MkdirAll(c.out, 0755); err != nil {
		return fmt.Errorf("could not create directory: %w", err)
	}

	dstName := filepath.Base(srcPath)
	dstPath := filepath.Join(c.out, dstName)

	if err := os.WriteFile(dstPath, []byte(code), 0644); err != nil {
		return fmt.Errorf("could not write file: %w", err)
	}

	return nil
}
