# json-transformer

## Why

Go's `encoding/json` gives you no control over the JSON output once your struct is defined. Real-world systems frequently need to change the JSON shape without touching the data model:

- A service receives `camelCase` JSON from a frontend but must forward `snake_case` to a downstream API.
- An API gateway needs to mask sensitive fields (passwords, tokens) before logging.
- A data pipeline reads NDJSON, renames fields to match a new schema, and streams the result to another sink.

The standard workarounds all have significant costs:

| Workaround | Problem |
|---|---|
| Second struct with different tags | Doubles type definitions; breaks with dynamic schemas |
| `json.Unmarshal` → rename → `json.Marshal` | Two full encode/decode passes; entire document loaded into memory |
| `map[string]any` manipulation | Verbose, error-prone, loses type information |
| Custom `MarshalJSON` per type | Couples serialisation logic to the domain model |

**json-transformer** solves this in one pass: rename keys and transform values as the JSON flows through, without loading the full document into memory.

## Design

**One pass, no round-trips.** When the input is a Go value (`map`, `struct`, `slice`), the library walks it directly and writes JSON with no intermediate marshal step. When the input is raw JSON (`[]byte`, `string`, `io.Reader`), a token-based decoder drives the output writer in lockstep — the document is never fully decoded into memory.

**Zero config, zero cost.** A `Transformer` with no options short-circuits to a direct pass-through. You pay only for what you configure.

**Concurrency-safe.** All per-call state is local to each invocation. One `Transformer` can be shared freely across goroutines.

**Follows `encoding/json` conventions.** Struct tags (`json:"name,omitempty"`, `json:"-"`, anonymous embedding) are respected. Map key order is non-deterministic, consistent with the standard library. Large numbers are preserved exactly via `json.Number` with no float64 precision loss.

## Installation

```bash
go get github.com/longbridge/json-transformer
```

## Quick start

```go
import jsontransform "github.com/longbridge/json-transformer"

t := jsontransform.New(
    jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()),
)

out, err := t.TransformBytes(map[string]any{"userName": "Alice", "userAge": 30})
// out: {"user_name":"Alice","user_age":30}
```

## API

### Creating a transformer

```go
t := jsontransform.New(opts ...Option) *Transformer
```

| Option | Description |
|---|---|
| `WithRenameFunc(fn RenameFunc)` | Called for every object key. Return `nil` to keep the original name, or `*string` with the new name. |
| `WithValueTransformer(field string, fn ValueTransformFunc)` | Called for the value of a specific field, matched by its **original** name (before any renaming). |

### Choosing a method

| Method | Use when |
|---|---|
| `TransformBytes(src any) ([]byte, error)` | You need the result as `[]byte`. The most common case. |
| `Transform(src any, dst io.Writer) error` | You already have an `io.Writer` to write into (HTTP response, file, network connection). Also the right choice when `src` is an `io.Reader`. |
| `TransformStream(r io.Reader, w io.Writer) error` | Your input contains **multiple JSON values** in sequence (NDJSON). Each value is transformed and written on its own line. Use `Transform` for single-value input. |

```go
out, err := t.TransformBytes(src)       // → []byte
err      := t.Transform(src, w)         // → io.Writer
err      := t.TransformStream(r, w)     // NDJSON: multiple values
```

### Input dispatch

`Transform` and `TransformBytes` accept any of the following as `src`:

| Type | Behaviour |
|---|---|
| `map[string]any` | Fast path — traverses the map directly, no marshal/unmarshal |
| `struct` / `*struct` | Fast path — reflection with cached field metadata |
| `slice` / `array` | Fast path |
| `[]byte` / `string` / `io.Reader` | Streaming path — parses JSON tokens without loading the full document |
| Primitives (`int`, `bool`, …) | Encoded directly with `encoding/json` |

## Built-in rename functions

```go
jsontransform.SnakeCaseRename()                          // "UserName"  → "user_name"
jsontransform.CamelCaseRename()                          // "user_name" → "userName"
jsontransform.PascalCaseRename()                         // "user_name" → "UserName"
jsontransform.KebabCaseRename()                          // "UserName"  → "user-name"
jsontransform.MapRename(map[string]string{"id": "ID"})   // exact lookup; returns nil if not found
```

`MapRename` returns `nil` for unknown keys, so it composes naturally with a fallback:

```go
exact    := jsontransform.MapRename(map[string]string{"id": "ID"})
fallback := jsontransform.SnakeCaseRename()

t := jsontransform.New(
    jsontransform.WithRenameFunc(func(name string) *string {
        if s := exact(name); s != nil {
            return s
        }
        return fallback(name)
    }),
)
```

## Examples

### Rename fields on a struct

```go
type User struct {
    UserName string `json:"userName"`
    UserAge  int    `json:"userAge"`
}

t := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
out, _ := t.TransformBytes(User{UserName: "Alice", UserAge: 30})
// {"user_name":"Alice","user_age":30}
```

### Rename a key and mask its value

`WithValueTransformer` is keyed by the **original** field name. Renaming and value transformation are independent and can be combined freely.

```go
t := jsontransform.New(
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

out, _ := t.TransformBytes(map[string]any{"user": "alice", "pwd": "secret"})
// {"user":"alice","password":"***"}
```

### Stream a large JSON file

```go
t := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.CamelCaseRename()))

f, _ := os.Open("large.json")
defer f.Close()
t.Transform(f, os.Stdout)
```

### Process NDJSON

```go
t := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))

r := strings.NewReader(`{"userId":1}` + "\n" + `{"userId":2}`)
t.TransformStream(r, os.Stdout)
// {"user_id":1}
// {"user_id":2}
```

## Notes

- **Field name matching** (both `WithValueTransformer` and `RenameFunc`) always uses the original field name, before any renaming is applied.
- **Value transformer input** when processing JSON text (`[]byte`/`string`/`io.Reader`): values arrive as `string`, `json.Number`, `bool`, `nil`, `map[string]any`, or `[]any`. When processing Go values directly, the original Go type is passed.
- **Value transformer output** is encoded as-is. It is not subject to further renaming or transformation.
- **Map key order** is non-deterministic, consistent with `encoding/json`.
- **Large numbers** in JSON text are preserved exactly via `json.Number`; there is no float64 precision loss.

## Benchmarks

Measured on Windows amd64, Intel Core i9-12900K, Go 1.24.

```
BenchmarkTransformMap          ~487 ns/op     1 alloc/op
BenchmarkTransformStruct       ~340 ns/op     2 allocs/op
BenchmarkTransformParallel     ~199 ns/op     1 alloc/op   (24 goroutines)
BenchmarkTransformBytes        ~899 ns/op    13 allocs/op  ([]byte input, fastjson path)
BenchmarkTransformNoOp          ~36 ns/op     1 alloc/op   (pass-through baseline, Writer)
BenchmarkTransformLargeJSON    ~975 ms/op   ~108 MB/s      (100 MB JSON array, []byte input)
```

The `[]byte`/`string`/`io.Reader` path is driven by [fastjson](https://github.com/valyala/fastjson)'s arena parser. The fast path (in-memory Go values) has no token-allocation overhead at all.

Run on your own machine:

```bash
go test -bench=. -benchmem ./...
```

## License

MIT — see [LICENSE](LICENSE).
