package lazyresolve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
)

var ResolversKey = "isutools.resolvers"

func ResolversMiddleware(withResolvers func(context.Context) (context.Context, error)) func(next echo.HandlerFunc) echo.HandlerFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := c.Request().Context()
			rctx, err := withResolvers(ctx)
			if err != nil {
				return fmt.Errorf("withResolvers: %w", err)
			}
			c.SetRequest(c.Request().WithContext(rctx))

			return next(c)
		}
	}
}

type JSONSerializer struct{}

func NewJSONSerializer() *JSONSerializer {
	return &JSONSerializer{}
}

func (j *JSONSerializer) Serialize(c echo.Context, i any, indent string) error {
	rs, err := GetResolvers[ResolveAller](c.Request().Context())
	if err != nil {
		return fmt.Errorf("failed to get resolvers: %w", err)
	}
	if err := rs.ResolveAll(c.Request().Context()); err != nil {
		return fmt.Errorf("failed to resolve resolvers: %w", err)
	}
	enc := json.NewEncoder(c.Response())
	return enc.Encode(i)
}

func (j *JSONSerializer) Deserialize(c echo.Context, i interface{}) error {
	err := json.NewDecoder(c.Request().Body).Decode(i)
	if ute, ok := err.(*json.UnmarshalTypeError); ok {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Unmarshal type error: expected=%v, got=%v, field=%v, offset=%v", ute.Type, ute.Value, ute.Field, ute.Offset)).SetInternal(err)
	} else if se, ok := err.(*json.SyntaxError); ok {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Syntax error: offset=%v, error=%v", se.Offset, se.Error())).SetInternal(err)
	}
	return err
}

func GetResolvers[T any](ctx context.Context) (T, error) {
	var zero T
	v := ctx.Value(ResolversKey)
	if v == nil {
		return zero, ErrResolverNotFound
	}
	return v.(T), nil
}

func WithResolvers(ctx context.Context, resolvers any) context.Context {
	return context.WithValue(ctx, ResolversKey, resolvers)
}

var ErrResolverNotFound = fmt.Errorf("resolver not found")

type ResolveAller interface {
	ResolveAll(context.Context) error
}

func ResolveAll(ctx context.Context, resolvers ...ResolverSubset) error {
	for range 10 {
		for _, r := range resolvers {
			if err := r.Resolve(ctx); err != nil {
				return err
			}
		}
		remain := lo.SumBy(resolvers, func(r ResolverSubset) int {
			return r.Count()
		})
		if remain == 0 {
			return nil
		}
	}
	errs := lo.FlatMap(resolvers, func(r ResolverSubset, _ int) []error {
		if r.Count() == 0 {
			return nil
		}
		return []error{fmt.Errorf("resolver=%s, count=%d", r.Name(), r.Count())}
	})
	return fmt.Errorf("has unresolved resolvers: %w", errors.Join(errs...))
}

type ResolverSubset interface {
	Resolve(context.Context) error
	Name() string
	Count() int
}

func NewResolver[T any, Key comparable](name string, resolve func(context.Context, []Key) ([]T, error)) Resolver[T, Key] {
	return &resolverImpl[T, Key]{_name: name, _resolve: resolve, resolvedMap: map[Key]T{}}
}

type resolverImpl[T any, Key comparable] struct {
	_name       string
	_resolve    func(context.Context, []Key) ([]T, error)
	futures     []*Future[T, Key]
	resolvedMap map[Key]T
}

func (r *resolverImpl[T, Key]) Resolve(ctx context.Context) error {
	if len(r.futures) == 0 {
		return nil
	}
	keys := lo.Map(r.futures, func(f *Future[T, Key], _ int) Key {
		return f.key
	})
	vs, err := r._resolve(ctx, keys)
	if err != nil {
		return err
	}
	for i, v := range vs {
		r.futures[i].resolvedCallback(v)
		r.resolvedMap[keys[i]] = v
	}
	r.futures = nil
	return nil
}

func (r *resolverImpl[T, Key]) Name() string {
	return r._name
}

func (r *resolverImpl[T, Key]) Future(key Key) *Future[T, Key] {
	if v, ok := r.resolvedMap[key]; ok {
		return NewResolvedFuture[T, Key](v)
	}
	f := &Future[T, Key]{resolver: r, key: key}
	r.futures = append(r.futures, f)
	return f
}

func (r *resolverImpl[T, Key]) Count() int {
	return len(r.futures)
}

type Resolver[T any, Key comparable] interface {
	Resolve(context.Context) error
	Name() string
	Future(key Key) *Future[T, Key]
	Count() int
}

type Future[T any, Key comparable] struct {
	resolver Resolver[T, Key]
	key      Key
	resolved bool
	value    T
}

func (f *Future[T, Key]) resolvedCallback(v T) {
	f.resolved = true
	f.value = v
}

var ErrNotResolved = fmt.Errorf("future not resolved")

func (f *Future[T, Key]) MarshalJSON() ([]byte, error) {
	if !f.resolved {
		return nil, fmt.Errorf("future not resolved: resolver=%s, key=%v, %w", f.resolver.Name(), f.key, ErrNotResolved)
	}
	return json.Marshal(f.value)
}

type Futures[T any, Key comparable] []*Future[T, Key]

func SortByIndex[T any, Key comparable](items []T, keys []Key, index func(T) Key) []T {
	keyToItem := lo.KeyBy(items, index)
	return lo.Map(keys, func(key Key, _ int) T {
		return keyToItem[key]
	})
}

func SortByIndexFallback[T any, Key comparable](items []T, keys []Key, index func(T) Key, fallback T) []T {
	keyToItem := lo.KeyBy(items, index)
	return lo.Map(keys, func(key Key, _ int) T {
		v, ok := keyToItem[key]
		if !ok {
			return fallback
		}
		return v
	})
}

func NewResolvedFuture[T any, Key comparable](v T) *Future[T, Key] {
	return &Future[T, Key]{resolved: true, value: v}
}
