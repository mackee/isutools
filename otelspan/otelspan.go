package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

type Opts struct {
	Fix      bool
	LogLevel slog.Level
}

func Run(ctx context.Context, from string, opts *Opts) error {
	dir, err := filepath.Abs(from)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: opts.LogLevel}))
	slog.SetDefault(logger)
	slog.DebugContext(ctx, "dir", slog.String("dir", dir))
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedImports | packages.NeedTypesInfo | packages.NeedName | packages.NeedModule,
		Dir:  dir,
	}, dir)
	if err != nil {
		return fmt.Errorf("failed to load package: %w", err)
	}
	pkgs = lo.Filter(pkgs, func(pkg *packages.Package, _ int) bool {
		return strings.HasPrefix(pkg.Module.Dir, dir)
	})

	for _, pkg := range pkgs {
		slog.DebugContext(ctx, "pkg", slog.String("path", pkg.PkgPath))
		for _, f := range pkg.Syntax {
			astutil.Apply(f, nil, func(c *astutil.Cursor) bool {
				n := c.Node()
				switch x := n.(type) {
				case *ast.FuncDecl:
					echoVar := false
					if list := x.Type.Params.List; len(list) > 0 {
						estimateCtx := list[0]
						switch estimateCtx.Names[0].Name {
						case "c":
							t, ok := estimateCtx.Type.(*ast.SelectorExpr)
							if !ok {
								return true
							}
							if n, ok := t.X.(*ast.Ident); ok && n.Name == "echo" && t.Sel.Name == "Context" {
								echoVar = true
							} else {
								return true
							}
						case "ctx":
							t, ok := estimateCtx.Type.(*ast.SelectorExpr)
							if !ok {
								return true
							}
							if n, ok := t.X.(*ast.Ident); ok && n.Name == "context" && t.Sel.Name == "Context" {
								// pass
							} else {
								return true
							}
						default:
							return true
						}
					} else {
						return true
					}
					if x.Doc != nil {
						for _, docc := range x.Doc.List {
							if docc.Text == "//elephandog:ignore-trace" {
								return true
							}
							if docc.Text == "//elephandog:append-trace" {
								return true
							}
						}
					}
					slog.DebugContext(ctx, "func", slog.String("name", x.Name.Name))
					if echoVar {
						for i, stmt := range x.Body.List {
							if astmt, ok := stmt.(*ast.AssignStmt); ok {
								ident := astmt.Lhs[0]
								if ident.(*ast.Ident).Name == "ctx" {
									x.Body.List = append(
										x.Body.List[:i+1],
										append(tracerStmts(x.Name.Name), x.Body.List[i+1:]...)...,
									)
									return true
								}
								return true
							}
						}
						x.Body.List = append(
							echoCtxAssignStmt(),
							append(tracerStmts(x.Name.Name), x.Body.List...)...,
						)
					} else {
						x.Body.List = append(
							tracerStmts(x.Name.Name),
							x.Body.List...,
						)
					}
					c.Replace(x)
				}
				return true
			})
			if !opts.Fix {
				continue
			}
			pos := pkg.Fset.Position(f.Pos())
			fullFilename := pos.Filename
			filename := strings.TrimPrefix(fullFilename, dir+"/")
			out, err := os.OpenFile(filename, os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}
			if err := func() error {
				defer out.Close()
				if err := format.Node(out, pkg.Fset, f); err != nil {
					return fmt.Errorf("failed to format node: %w", err)
				}
				return nil
			}(); err != nil {
				return err
			}
		}
	}

	return nil
}

func tracerStmts(name string) []ast.Stmt {
	return []ast.Stmt{
		&ast.AssignStmt{
			Lhs: []ast.Expr{
				&ast.Ident{Name: "_"},
				&ast.Ident{Name: "span"},
			},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{
				&ast.CallExpr{
					Fun: &ast.SelectorExpr{
						X:   &ast.Ident{Name: "tracer"},
						Sel: &ast.Ident{Name: "Start"},
					},
					Args: []ast.Expr{
						&ast.Ident{Name: "ctx"},
						&ast.BasicLit{Value: fmt.Sprintf("%q", name)},
					},
				},
			},
		},
		&ast.DeferStmt{
			Call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "span"},
					Sel: &ast.Ident{Name: "End"},
				},
			},
		},
	}
}

func echoCtxAssignStmt() []ast.Stmt {
	return []ast.Stmt{
		&ast.AssignStmt{
			Lhs: []ast.Expr{
				&ast.Ident{Name: "ctx"},
			},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{
				&ast.CallExpr{
					Fun: &ast.SelectorExpr{
						X: &ast.CallExpr{
							Fun: &ast.SelectorExpr{
								X: &ast.Ident{
									Name: "c",
								},
								Sel: &ast.Ident{
									Name: "Request",
								},
							},
						},
						Sel: &ast.Ident{
							Name: "Context",
						},
					},
				},
			},
		},
	}
}
