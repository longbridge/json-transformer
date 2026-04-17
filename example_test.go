package jsontransform_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	jsontransform "github.com/longbridge/json-transformer"
)

// ExampleNew_renameSnakeCase demonstrates renaming camelCase keys to snake_case.
func ExampleNew_renameSnakeCase() {
	t := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	out, _ := t.TransformBytes(map[string]any{"userName": "Alice", "userAge": 30})
	fmt.Println(string(out))
	// Note: map key order is non-deterministic; output may vary.
}

// ExampleNew_maskField demonstrates renaming a field and masking its value.
func ExampleNew_maskField() {
	t := jsontransform.New(
		jsontransform.WithRenameFunc(func(name string) *string {
			if name == "pwd" {
				s := "password"
				return &s
			}
			return nil
		}),
		jsontransform.WithValueTransformer(func(name string) jsontransform.ValueTransformFunc {
			if name == "pwd" {
				return func(v any) any { return "***" }
			}
			return nil
		}),
	)
	out, _ := t.TransformBytes(`{"user":"alice","pwd":"secret"}`)
	fmt.Println(string(out))
	// Output: {"user":"alice","password":"***"}
}

// ExampleNew_patternMatch demonstrates matching fields by suffix to reformat timestamps.
func ExampleNew_patternMatch() {
	t := jsontransform.New(
		jsontransform.WithValueTransformer(func(name string) jsontransform.ValueTransformFunc {
			if strings.HasSuffix(name, "_at") {
				return func(v any) any {
					if n, ok := v.(json.Number); ok {
						ts, _ := n.Int64()
						return time.Unix(ts, 0).UTC().Format(time.RFC3339)
					}
					return v
				}
			}
			return nil
		}),
	)
	out, _ := t.TransformBytes(`{"name":"Alice","created_at":1700000000}`)
	fmt.Println(string(out))
	// Output: {"name":"Alice","created_at":"2023-11-14T22:13:20Z"}
}

// ExampleTransformer_TransformStream demonstrates processing NDJSON line by line.
func ExampleTransformer_TransformStream() {
	t := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))

	r := strings.NewReader(`{"userId":1}` + "\n" + `{"userId":2}`)
	var buf strings.Builder
	_ = t.TransformStream(r, &buf)
	fmt.Println(buf.String())
	// Output:
	// {"user_id":1}
	// {"user_id":2}
}

// ExampleNew_mapRenameWithFallback demonstrates combining MapRename with a fallback.
func ExampleNew_mapRenameWithFallback() {
	exact := jsontransform.MapRename(map[string]string{"id": "ID"})
	fallback := jsontransform.SnakeCaseRename()

	t := jsontransform.New(
		jsontransform.WithRenameFunc(func(name string) *string {
			if s := exact(name); s != nil {
				return s
			}
			return fallback(name)
		}),
	)
	out, _ := t.TransformBytes(`{"id":1,"userName":"Alice"}`)
	fmt.Println(string(out))
	// Output: {"ID":1,"user_name":"Alice"}
}
