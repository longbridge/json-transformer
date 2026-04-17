package jsontransform_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	jsontransform "github.com/longbridge/json-transformer"
)

// helper: transform src and return output string.
func transform(t *testing.T, tr *jsontransform.Transformer, src any) string {
	t.Helper()
	out, err := tr.TransformBytes(src)
	if err != nil {
		t.Fatalf("TransformBytes error: %v", err)
	}
	return string(out)
}

// helper: assert JSON equality (ignoring key order differences).
func assertJSONEqual(t *testing.T, expected, actual string) {
	t.Helper()
	var e, a any
	if err := json.Unmarshal([]byte(expected), &e); err != nil {
		t.Fatalf("expected is invalid JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(actual), &a); err != nil {
		t.Fatalf("actual is invalid JSON %q: %v", actual, err)
	}
	eb, _ := json.Marshal(e)
	ab, _ := json.Marshal(a)
	if string(eb) != string(ab) {
		t.Errorf("JSON mismatch:\n  expected: %s\n  actual:   %s", expected, actual)
	}
}

// ─── No-op (pass-through) ────────────────────────────────────────────────────

func TestNoOp_Map(t *testing.T) {
	tr := jsontransform.New()
	got := transform(t, tr, map[string]any{"a": 1, "b": "hello"})
	assertJSONEqual(t, `{"a":1,"b":"hello"}`, got)
}

func TestNoOp_String(t *testing.T) {
	tr := jsontransform.New()
	var buf bytes.Buffer
	if err := tr.Transform(`{"key":"value"}`, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != `{"key":"value"}` {
		t.Errorf("unexpected: %s", buf.String())
	}
}

func TestNoOp_Bytes(t *testing.T) {
	tr := jsontransform.New()
	var buf bytes.Buffer
	input := []byte(`{"key":"value"}`)
	if err := tr.Transform(input, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != `{"key":"value"}` {
		t.Errorf("unexpected: %s", buf.String())
	}
}

func TestNoOp_Reader(t *testing.T) {
	tr := jsontransform.New()
	var buf bytes.Buffer
	r := strings.NewReader(`{"key":"value"}`)
	if err := tr.Transform(r, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != `{"key":"value"}` {
		t.Errorf("unexpected: %s", buf.String())
	}
}

// ─── RenameFunc ──────────────────────────────────────────────────────────────

func TestRename_SnakeCase_Map(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, map[string]any{"userName": "Alice", "userAge": 30})
	assertJSONEqual(t, `{"user_name":"Alice","user_age":30}`, got)
}

func TestRename_SnakeCase_String(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, `{"userName":"Alice","userAge":30}`)
	assertJSONEqual(t, `{"user_name":"Alice","user_age":30}`, got)
}

func TestRename_CamelCase(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.CamelCaseRename()))
	got := transform(t, tr, `{"user_name":"Alice","user_age":30}`)
	assertJSONEqual(t, `{"userName":"Alice","userAge":30}`, got)
}

func TestRename_PascalCase(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.PascalCaseRename()))
	got := transform(t, tr, `{"user_name":"Alice"}`)
	assertJSONEqual(t, `{"UserName":"Alice"}`, got)
}

func TestRename_KebabCase(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.KebabCaseRename()))
	got := transform(t, tr, `{"UserName":"Alice"}`)
	assertJSONEqual(t, `{"user-name":"Alice"}`, got)
}

func TestRename_MapRename(t *testing.T) {
	tr := jsontransform.New(
		jsontransform.WithRenameFunc(jsontransform.MapRename(map[string]string{"id": "ID", "name": "fullName"})),
	)
	got := transform(t, tr, map[string]any{"id": 1, "name": "Alice", "age": 30})
	assertJSONEqual(t, `{"ID":1,"fullName":"Alice","age":30}`, got)
}

func TestRename_MapRename_FallbackToSnake(t *testing.T) {
	exact := jsontransform.MapRename(map[string]string{"id": "ID"})
	fallback := jsontransform.SnakeCaseRename()
	tr := jsontransform.New(
		jsontransform.WithRenameFunc(func(name string) *string {
			if s := exact(name); s != nil {
				return s
			}
			return fallback(name)
		}),
	)
	got := transform(t, tr, map[string]any{"id": 1, "userName": "Alice"})
	assertJSONEqual(t, `{"ID":1,"user_name":"Alice"}`, got)
}

func TestRename_CustomFunc(t *testing.T) {
	tr := jsontransform.New(
		jsontransform.WithRenameFunc(func(name string) *string {
			if name == "pwd" {
				s := "password"
				return &s
			}
			return nil
		}),
	)
	got := transform(t, tr, map[string]any{"user": "alice", "pwd": "secret"})
	assertJSONEqual(t, `{"user":"alice","password":"secret"}`, got)
}

// ─── ValueTransformFunc ──────────────────────────────────────────────────────

func TestValueTransformer_Mask(t *testing.T) {
	tr := jsontransform.New(
		jsontransform.WithRenameFunc(func(name string) *string {
			if name == "pwd" {
				s := "password"
				return &s
			}
			return nil
		}),
		jsontransform.WithValueTransformer("pwd", func(v any) any {
			return "***"
		}),
	)
	got := transform(t, tr, map[string]any{"user": "alice", "pwd": "secret"})
	assertJSONEqual(t, `{"user":"alice","password":"***"}`, got)
}

func TestValueTransformer_StreamPath(t *testing.T) {
	tr := jsontransform.New(
		jsontransform.WithValueTransformer("score", func(v any) any {
			if n, ok := v.(json.Number); ok {
				f, _ := n.Float64()
				return f * 2
			}
			return v
		}),
	)
	got := transform(t, tr, `{"name":"Alice","score":50}`)
	assertJSONEqual(t, `{"name":"Alice","score":100}`, got)
}

// ─── Struct fast path ────────────────────────────────────────────────────────

type User struct {
	UserName string `json:"userName"`
	UserAge  int    `json:"userAge"`
}

func TestStruct_SnakeCase(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, User{UserName: "Alice", UserAge: 30})
	assertJSONEqual(t, `{"user_name":"Alice","user_age":30}`, got)
}

func TestStructPtr_SnakeCase(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, &User{UserName: "Bob", UserAge: 25})
	assertJSONEqual(t, `{"user_name":"Bob","user_age":25}`, got)
}

type UserWithOmit struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

func TestStruct_Omitempty(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, UserWithOmit{Name: "Alice"})
	assertJSONEqual(t, `{"name":"Alice"}`, got)
}

type UserWithSkip struct {
	Name     string `json:"name"`
	Internal string `json:"-"`
}

func TestStruct_SkipField(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, UserWithSkip{Name: "Alice", Internal: "hidden"})
	assertJSONEqual(t, `{"name":"Alice"}`, got)
}

type Address struct {
	City    string `json:"city"`
	Country string `json:"country"`
}

type UserWithAddress struct {
	Name    string  `json:"name"`
	Address Address `json:"address"`
}

func TestStruct_Nested(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, UserWithAddress{
		Name:    "Alice",
		Address: Address{City: "NYC", Country: "US"},
	})
	assertJSONEqual(t, `{"name":"Alice","address":{"city":"NYC","country":"US"}}`, got)
}

type Base struct {
	ID int `json:"id"`
}

type ExtendedUser struct {
	Base
	Name string `json:"name"`
}

func TestStruct_Embedded(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, ExtendedUser{Base: Base{ID: 1}, Name: "Alice"})
	assertJSONEqual(t, `{"id":1,"name":"Alice"}`, got)
}

// ─── Slice / array paths ─────────────────────────────────────────────────────

func TestSlice_Map(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	input := []any{
		map[string]any{"firstName": "Alice"},
		map[string]any{"firstName": "Bob"},
	}
	got := transform(t, tr, input)
	assertJSONEqual(t, `[{"first_name":"Alice"},{"first_name":"Bob"}]`, got)
}

func TestSlice_StringInput(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, `[{"firstName":"Alice"},{"firstName":"Bob"}]`)
	assertJSONEqual(t, `[{"first_name":"Alice"},{"first_name":"Bob"}]`, got)
}

// ─── Edge cases ──────────────────────────────────────────────────────────────

func TestEdge_NullInput(t *testing.T) {
	tr := jsontransform.New()
	var buf bytes.Buffer
	if err := tr.Transform(nil, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "null" {
		t.Errorf("expected null, got %s", buf.String())
	}
}

func TestEdge_EmptyObject(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, map[string]any{})
	if got != "{}" {
		t.Errorf("expected {}, got %s", got)
	}
}

func TestEdge_EmptyArray(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, `[]`)
	if got != "[]" {
		t.Errorf("expected [], got %s", got)
	}
}

func TestEdge_NullValue(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, map[string]any{"myField": nil})
	assertJSONEqual(t, `{"my_field":null}`, got)
}

func TestEdge_DeepNesting(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	input := `{"levelOne":{"levelTwo":{"levelThree":{"deepKey":"value"}}}}`
	got := transform(t, tr, input)
	assertJSONEqual(t, `{"level_one":{"level_two":{"level_three":{"deep_key":"value"}}}}`, got)
}

func TestEdge_TopLevelString(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, `"hello"`)
	if got != `"hello"` {
		t.Errorf("expected \"hello\", got %s", got)
	}
}

func TestEdge_TopLevelNumber(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, `42`)
	if got != `42` {
		t.Errorf("expected 42, got %s", got)
	}
}

func TestEdge_TopLevelNull(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	got := transform(t, tr, `null`)
	if got != `null` {
		t.Errorf("expected null, got %s", got)
	}
}

func TestEdge_LargeNumber_Precision(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	// This number would lose precision if converted to float64.
	input := `{"bigNum":9999999999999999999}`
	got := transform(t, tr, input)
	// Key is renamed; value must be preserved exactly.
	assertJSONEqual(t, `{"big_num":9999999999999999999}`, got)
}

// ─── TransformStream (NDJSON) ────────────────────────────────────────────────

func TestTransformStream_NDJSON(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	r := strings.NewReader(`{"userId":1}` + "\n" + `{"userId":2}`)
	var buf bytes.Buffer
	if err := tr.TransformStream(r, &buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(buf.String(), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
	assertJSONEqual(t, `{"user_id":1}`, lines[0])
	assertJSONEqual(t, `{"user_id":2}`, lines[1])
}

func TestTransformStream_Single(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	r := strings.NewReader(`{"userId":1}`)
	var buf bytes.Buffer
	if err := tr.TransformStream(r, &buf); err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, `{"user_id":1}`, buf.String())
}

// ─── Escape characters ───────────────────────────────────────────────────────

func TestEscape_StreamPath(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))

	cases := []struct{ name, input string }{
		{"newline in value",   `{"firstName":"Alice\nSmith"}`},
		{"tab in value",       `{"firstName":"Alice\tSmith"}`},
		{"quote in value",     `{"firstName":"Alice\"Smith"}`},
		{"backslash in value", `{"firstName":"Alice\\Smith"}`},
		{"control u0001",      `{"firstName":"Alice\u0001Smith"}`},
		{"null byte u0000",    `{"firstName":"Alice\u0000Smith"}`},
		{"unicode CJK",        `{"firstName":"\u4e2d\u6587"}`},
		{"emoji surrogate",    `{"firstName":"\uD83D\uDE00"}`},
		{"backslash in key",   `{"first\\Name":"Alice"}`},
		{"unicode in key",     `{"first\u004Eame":"Alice"}`},
		{"nested escapes",     `{"outerKey":{"innerKey":"line1\nline2"}}`},
		{"chinese chars",      `{"用户名":"张三","年龄":30}`},
		{"html chars",         `{"url":"https://x.com?a=1&b=<2>"}`},
		{"array with escapes", `[{"firstName":"Alice\nBob"},{"firstName":"Carol\tDave"}]`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tr.TransformBytes(tc.input)
			if err != nil {
				t.Fatalf("error: %v\ninput: %s", err, tc.input)
			}
			var v any
			if err := json.Unmarshal(out, &v); err != nil {
				t.Fatalf("output is invalid JSON: %v\ninput:  %s\noutput: %s", err, tc.input, out)
			}
		})
	}
}

func TestEscape_FastPath(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))

	cases := []struct {
		name string
		src  any
	}{
		{"control char in value", map[string]any{"firstName": "Alice\x01Smith"}},
		{"tab in key",            map[string]any{"first\tName": "Alice"}},
		{"backslash in value",    map[string]any{"fileName": `C:\Users\Alice`}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tr.TransformBytes(tc.src)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			var v any
			if err := json.Unmarshal(out, &v); err != nil {
				t.Fatalf("output is invalid JSON: %v\noutput: %s", err, out)
			}
		})
	}
}

// ─── Concurrency safety ──────────────────────────────────────────────────────

func TestConcurrency(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			got, err := tr.TransformBytes(map[string]any{"firstName": "Alice", "lastName": "Smith"})
			if err != nil {
				t.Errorf("error: %v", err)
				return
			}
			var m map[string]any
			if err := json.Unmarshal(got, &m); err != nil {
				t.Errorf("invalid JSON: %v", err)
				return
			}
			if _, ok := m["first_name"]; !ok {
				t.Errorf("missing first_name key in %s", string(got))
			}
		}()
	}
	wg.Wait()
}

func TestConcurrency_StreamPath(t *testing.T) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			got, err := tr.TransformBytes(`{"firstName":"Alice","lastName":"Smith"}`)
			if err != nil {
				t.Errorf("error: %v", err)
				return
			}
			var m map[string]any
			if err := json.Unmarshal(got, &m); err != nil {
				t.Errorf("invalid JSON: %v", err)
				return
			}
			if _, ok := m["first_name"]; !ok {
				t.Errorf("missing first_name in %s", string(got))
			}
		}()
	}
	wg.Wait()
}
