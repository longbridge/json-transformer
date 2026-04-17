// Package jsontransform provides streaming JSON transformation with field renaming and value transformation.
package jsontransform

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"sync"
)

// RenameFunc is called for each object field name. Return nil to keep the original name,
// or a *string with the new name to rename it.
type RenameFunc func(name string) *string

// ValueTransformFunc transforms a decoded Go value (string/float64/bool/nil/map/slice).
type ValueTransformFunc func(value any) any

// renameResult is the cached outcome of a RenameFunc call.
type renameResult struct {
	name string
	omit bool
}

// Transformer performs JSON transformation with optional field renaming and value transformation.
// A Transformer is safe for concurrent use after construction.
type Transformer struct {
	renameFunc        RenameFunc
	valueTransformers map[string]ValueTransformFunc
	// renameCache memoises RenameFunc results keyed by original field name.
	// RenameFunc is assumed to be deterministic (same input → same output).
	renameCache sync.Map // map[string]renameResult
}

// Option configures a Transformer.
type Option func(*Transformer)

// WithRenameFunc sets a function to rename object field names.
func WithRenameFunc(fn RenameFunc) Option {
	return func(t *Transformer) {
		t.renameFunc = fn
	}
}

// WithValueTransformer registers a transformation function for a specific field name (original name).
func WithValueTransformer(fieldName string, fn ValueTransformFunc) Option {
	return func(t *Transformer) {
		if t.valueTransformers == nil {
			t.valueTransformers = make(map[string]ValueTransformFunc)
		}
		t.valueTransformers[fieldName] = fn
	}
}

// New creates a new Transformer with the given options.
func New(opts ...Option) *Transformer {
	t := &Transformer{}
	for _, o := range opts {
		o(t)
	}
	return t
}

// bufPool pools bare bytes.Buffer for TransformBytes' outer result buffer.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Transform transforms src and writes the result to dst.
//
// src dispatch:
//   - map[string]any        → fast path (in-memory traversal, no marshal/unmarshal)
//   - struct / *struct      → fast path (reflection with field-metadata cache)
//   - slice / array         → fast path
//   - []byte / string / io.Reader → streaming path (token-based)
//   - other primitive types → encoded directly
func (t *Transformer) Transform(src any, dst io.Writer) error {
	if src == nil {
		_, err := io.WriteString(dst, "null")
		return err
	}

	// No-op short-circuit: pass through without any transformation.
	if t.renameFunc == nil && len(t.valueTransformers) == 0 {
		switch v := src.(type) {
		case []byte:
			_, err := dst.Write(v)
			return err
		case string:
			_, err := io.WriteString(dst, v)
			return err
		case io.Reader:
			_, err := io.Copy(dst, v)
			return err
		default:
			enc := json.NewEncoder(dst)
			enc.SetEscapeHTML(false)
			return enc.Encode(v)
		}
	}

	switch v := src.(type) {
	case map[string]any:
		return t.transformGoValue(v, dst)
	case []byte:
		return t.transformFastJSONBytes(v, dst)
	case string:
		return t.transformFastJSONBytes([]byte(v), dst)
	case io.Reader:
		raw, err := io.ReadAll(v)
		if err != nil {
			return err
		}
		return t.transformFastJSONBytes(raw, dst)
	default:
		rv := reflect.ValueOf(src)
		switch rv.Kind() {
		case reflect.Struct:
			return t.transformGoValue(src, dst)
		case reflect.Ptr:
			if rv.IsNil() {
				_, err := io.WriteString(dst, "null")
				return err
			}
			if rv.Elem().Kind() == reflect.Struct {
				return t.transformGoValue(src, dst)
			}
		case reflect.Slice, reflect.Array:
			return t.transformGoValue(src, dst)
		}
		enc := json.NewEncoder(dst)
		enc.SetEscapeHTML(false)
		return enc.Encode(src)
	}
}

// TransformBytes transforms src and returns the result as []byte.
// It uses pooled buffers for efficiency.
func (t *Transformer) TransformBytes(src any) ([]byte, error) {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	if err := t.Transform(src, buf); err != nil {
		return nil, err
	}

	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

// TransformStream processes multiple whitespace-separated JSON values from r and writes
// transformed results to w, one per line (suitable for NDJSON).
func (t *Transformer) TransformStream(r io.Reader, w io.Writer) error {
	dec := json.NewDecoder(r)
	dec.UseNumber()

	first := true
	for dec.More() {
		if !first {
			if _, err := io.WriteString(w, "\n"); err != nil {
				return err
			}
		}
		first = false

		sw := newStreamWriter(w)
		if err := t.transformTokenValue(dec, sw); err != nil {
			return err
		}
		if err := sw.flush(); err != nil {
			return err
		}
	}
	return nil
}

// applyRename applies the rename function to a field name, with memoisation.
// Returns the (possibly renamed) key and whether it should be omitted.
func (t *Transformer) applyRename(name string) (string, bool) {
	if t.renameFunc == nil {
		return name, false
	}
	if v, ok := t.renameCache.Load(name); ok {
		r := v.(renameResult)
		return r.name, r.omit
	}
	result := t.renameFunc(name)
	var r renameResult
	switch {
	case result == nil:
		r = renameResult{name: name}
	case *result == "":
		r = renameResult{omit: true}
	default:
		r = renameResult{name: *result}
	}
	t.renameCache.Store(name, r)
	return r.name, r.omit
}
