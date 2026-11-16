package filterlang

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/alphaleonis/nibs/internal/graph/model"
)

// nibFilterIndex maps each NibFilter field's GraphQL name to its index in the
// struct. Building it panics when a token table names a key that NibFilter
// lacks or declares with another Go type, so a bad table fails at startup.
var nibFilterIndex = func() map[string]int {
	typ := reflect.TypeFor[model.NibFilter]()
	index := make(map[string]int, typ.NumField())
	for i := range typ.NumField() {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		index[name] = i
	}
	check := func(key string, want reflect.Type) {
		i, ok := index[key]
		if !ok || typ.Field(i).Type != want {
			panic(fmt.Sprintf("filterlang: NibFilter has no %v field %q", want, key))
		}
	}
	for _, f := range fields {
		check(f.FilterKey, reflect.TypeFor[[]string]())
		check(f.ExcludeKey, reflect.TypeFor[[]string]())
	}
	for _, r := range relTokens {
		if r.Kind == RelID {
			check(r.Field, reflect.TypeFor[*string]())
		} else {
			check(r.Field, reflect.TypeFor[*bool]())
		}
	}
	for _, a := range areaTokens {
		check(a.Field, reflect.TypeFor[*string]())
	}
	return index
}()

// fieldOf returns a pointer to the field of f with GraphQL name key. T must be
// the field's Go type.
func fieldOf[T any](f *model.NibFilter, key string) *T {
	return reflect.ValueOf(f).Elem().Field(nibFilterIndex[key]).Addr().Interface().(*T)
}
