package jsontransform

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// structFieldCache caches parsed struct field metadata to avoid repeated reflection.
var structFieldCache sync.Map // map[reflect.Type][]structField

type structField struct {
	index     int
	jsonName  string
	omitempty bool
	skip      bool
	anonymous bool
	isPtr     bool
}

// transformGoValue dispatches to the appropriate fast-path writer.
func (t *Transformer) transformGoValue(v any, w io.Writer) error {
	bw := newBufWriter(w)
	t.writeGoValue(v, bw)
	return bw.flush()
}

// writeGoValue recursively writes a Go value to bw.
func (t *Transformer) writeGoValue(v any, bw *bufWriter) {
	if v == nil {
		bw.writeRaw("null")
		return
	}

	switch val := v.(type) {
	case map[string]any:
		t.writeGoMap(val, bw)
	case []any:
		t.writeGoSlice(val, bw)
	case string:
		bw.writeJSONString(val)
	case bool:
		if val {
			bw.writeRaw("true")
		} else {
			bw.writeRaw("false")
		}
	case json.Number:
		bw.writeRaw(val.String())
	case float64:
		bw.writeFloat64(val)
	case int:
		bw.writeInt64(int64(val))
	case int8:
		bw.writeInt64(int64(val))
	case int16:
		bw.writeInt64(int64(val))
	case int32:
		bw.writeInt64(int64(val))
	case int64:
		bw.writeInt64(val)
	case uint:
		bw.writeUint64(uint64(val))
	case uint8:
		bw.writeUint64(uint64(val))
	case uint16:
		bw.writeUint64(uint64(val))
	case uint32:
		bw.writeUint64(uint64(val))
	case uint64:
		bw.writeUint64(val)
	case float32:
		bw.writeFloat64(float64(val))
	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Struct:
			t.writeGoStruct(rv, bw)
		case reflect.Ptr:
			if rv.IsNil() {
				bw.writeRaw("null")
			} else {
				t.writeGoValue(rv.Elem().Interface(), bw)
			}
		case reflect.Slice:
			if rv.IsNil() {
				bw.writeRaw("null")
				return
			}
			t.writeGoReflectSlice(rv, bw)
		case reflect.Array:
			t.writeGoReflectSlice(rv, bw)
		case reflect.Map:
			t.writeGoReflectMap(rv, bw)
		default:
			bw.writeJSONEncoded(v)
		}
	}
}

func (t *Transformer) writeGoMap(m map[string]any, bw *bufWriter) {
	bw.writeRaw("{")
	first := true
	for originalKey, val := range m {
		newKey, omit := t.applyRename(originalKey)
		if omit {
			continue
		}
		if fn := t.applyValueTransformer(originalKey); fn != nil {
			val = fn(val)
		}
		if !first {
			bw.writeRaw(",")
		}
		first = false
		bw.writeKey(newKey)
		t.writeGoValue(val, bw)
	}
	bw.writeRaw("}")
}

func (t *Transformer) writeGoSlice(s []any, bw *bufWriter) {
	bw.writeRaw("[")
	for i, val := range s {
		if i > 0 {
			bw.writeRaw(",")
		}
		t.writeGoValue(val, bw)
	}
	bw.writeRaw("]")
}

func (t *Transformer) writeGoReflectSlice(rv reflect.Value, bw *bufWriter) {
	bw.writeRaw("[")
	n := rv.Len()
	for i := range n {
		if i > 0 {
			bw.writeRaw(",")
		}
		t.writeGoValue(rv.Index(i).Interface(), bw)
	}
	bw.writeRaw("]")
}

func (t *Transformer) writeGoReflectMap(rv reflect.Value, bw *bufWriter) {
	bw.writeRaw("{")
	first := true
	for _, key := range rv.MapKeys() {
		originalKey := key.String()
		newKey, omit := t.applyRename(originalKey)
		if omit {
			continue
		}
		val := rv.MapIndex(key).Interface()
		if fn := t.applyValueTransformer(originalKey); fn != nil {
			val = fn(val)
		}
		if !first {
			bw.writeRaw(",")
		}
		first = false
		bw.writeKey(newKey)
		t.writeGoValue(val, bw)
	}
	bw.writeRaw("}")
}

func getStructFields(rt reflect.Type) []structField {
	if cached, ok := structFieldCache.Load(rt); ok {
		return cached.([]structField)
	}
	fields := make([]structField, 0, rt.NumField())
	for i := range rt.NumField() {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		sf := structField{
			index:     i,
			jsonName:  f.Name,
			anonymous: f.Anonymous,
			isPtr:     f.Type.Kind() == reflect.Ptr,
		}
		tag := f.Tag.Get("json")
		if tag != "" {
			name, opts, _ := strings.Cut(tag, ",")
			if name == "-" && opts == "" {
				sf.skip = true
			} else if name == "-" {
				sf.jsonName = "-"
			} else if name != "" {
				sf.jsonName = name
			}
			if strings.Contains(opts, "omitempty") {
				sf.omitempty = true
			}
		}
		fields = append(fields, sf)
	}
	structFieldCache.Store(rt, fields)
	return fields
}

func (t *Transformer) writeGoStruct(rv reflect.Value, bw *bufWriter) {
	bw.writeRaw("{")
	first := true
	t.writeStructFields(rv, bw, &first)
	bw.writeRaw("}")
}

func (t *Transformer) writeStructFields(rv reflect.Value, bw *bufWriter, first *bool) {
	fields := getStructFields(rv.Type())
	for _, sf := range fields {
		if sf.skip {
			continue
		}
		fieldVal := rv.Field(sf.index)
		if sf.anonymous {
			fv := fieldVal
			if sf.isPtr {
				if fv.IsNil() {
					continue
				}
				fv = fv.Elem()
			}
			if fv.Kind() == reflect.Struct {
				t.writeStructFields(fv, bw, first)
				continue
			}
		}
		if sf.omitempty && isZeroValue(fieldVal) {
			continue
		}
		newKey, omit := t.applyRename(sf.jsonName)
		if omit {
			continue
		}
		iface := fieldVal.Interface()
		if fn := t.applyValueTransformer(sf.jsonName); fn != nil {
			iface = fn(iface)
		}
		if !*first {
			bw.writeRaw(",")
		}
		*first = false
		bw.writeKey(newKey)
		t.writeGoValue(iface, bw)
	}
}

func isZeroValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.String:
		return v.Len() == 0
	case reflect.Slice, reflect.Map, reflect.Ptr, reflect.Interface:
		return v.IsNil()
	case reflect.Array:
		return v.Len() == 0
	}
	return false
}

// ── bufWriter ────────────────────────────────────────────────────────────────

// bufWriterPool pools bufWriter structs including their buffer and fallback encoder.
var bufWriterPool = sync.Pool{
	New: func() any {
		buf := new(bytes.Buffer)
		bw := &bufWriter{buf: buf}
		bw.enc = json.NewEncoder(buf)
		bw.enc.SetEscapeHTML(false)
		return bw
	},
}

// bufWriter batches writes through a pooled bytes.Buffer.
// Use newBufWriter / flush to acquire and release.
type bufWriter struct {
	w   io.Writer
	buf *bytes.Buffer
	enc *json.Encoder // fallback for unknown/complex types
	err error
}

func newBufWriter(w io.Writer) *bufWriter {
	bw := bufWriterPool.Get().(*bufWriter)
	bw.buf.Reset()
	bw.w = w
	bw.err = nil
	return bw
}

func (bw *bufWriter) flush() error {
	err := bw.err
	if err == nil {
		_, err = bw.buf.WriteTo(bw.w)
	}
	bw.w = nil
	bw.err = nil
	bufWriterPool.Put(bw)
	return err
}

func (bw *bufWriter) writeRaw(s string) {
	if bw.err != nil {
		return
	}
	_, bw.err = io.WriteString(bw.buf, s)
}

// writeJSONString writes s as a JSON string directly to the buffer without going
// through json.Encoder, eliminating the intermediate encodeState allocation.
func (bw *bufWriter) writeJSONString(s string) {
	if bw.err != nil {
		return
	}
	b := bw.buf.AvailableBuffer()
	b = appendJSONString(b, s)
	_, bw.err = bw.buf.Write(b)
}

func (bw *bufWriter) writeKey(key string) {
	bw.writeJSONString(key)
	bw.writeRaw(":")
}

func (bw *bufWriter) writeInt64(v int64) {
	if bw.err != nil {
		return
	}
	b := bw.buf.AvailableBuffer()
	b = strconv.AppendInt(b, v, 10)
	_, bw.err = bw.buf.Write(b)
}

func (bw *bufWriter) writeUint64(v uint64) {
	if bw.err != nil {
		return
	}
	b := bw.buf.AvailableBuffer()
	b = strconv.AppendUint(b, v, 10)
	_, bw.err = bw.buf.Write(b)
}

func (bw *bufWriter) writeFloat64(v float64) {
	if bw.err != nil {
		return
	}
	b := bw.buf.AvailableBuffer()
	b = strconv.AppendFloat(b, v, 'f', -1, 64)
	_, bw.err = bw.buf.Write(b)
}

// writeJSONEncoded uses the pooled json.Encoder as a fallback for unknown types.
func (bw *bufWriter) writeJSONEncoded(v any) {
	if bw.err != nil {
		return
	}
	before := bw.buf.Len()
	bw.err = bw.enc.Encode(v)
	if bw.err == nil {
		// Strip the trailing newline added by json.Encoder.
		if bw.buf.Len() > before && bw.buf.Bytes()[bw.buf.Len()-1] == '\n' {
			bw.buf.Truncate(bw.buf.Len() - 1)
		}
	}
}

// ── JSON string encoding ─────────────────────────────────────────────────────

const hexChars = "0123456789abcdef"

// appendJSONString appends the JSON encoding of s (with surrounding quotes) to dst.
// Assumes valid UTF-8 input. Does not escape non-ASCII characters (SetEscapeHTML=false semantics).
func appendJSONString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		b := s[i]
		if b < utf8.RuneSelf {
			// ASCII: only control chars, '"', and '\' need escaping.
			if b >= 0x20 && b != '"' && b != '\\' {
				i++
				continue
			}
			dst = append(dst, s[start:i]...)
			start = i + 1
			switch b {
			case '"':
				dst = append(dst, '\\', '"')
			case '\\':
				dst = append(dst, '\\', '\\')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				dst = append(dst, '\\', 'u', '0', '0',
					hexChars[b>>4], hexChars[b&0xf])
			}
			i++
		} else {
			// Multi-byte UTF-8 rune: pass through as-is.
			_, size := utf8.DecodeRuneInString(s[i:])
			i += size
		}
	}
	dst = append(dst, s[start:]...)
	dst = append(dst, '"')
	return dst
}
