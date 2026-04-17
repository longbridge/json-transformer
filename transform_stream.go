package jsontransform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// transformTokenValue reads one complete JSON value from dec and writes it transformed to sw.
func (t *Transformer) transformTokenValue(dec *json.Decoder, sw *streamWriter) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			return t.transformTokenObject(dec, sw)
		case '[':
			return t.transformTokenArray(dec, sw)
		default:
			return fmt.Errorf("unexpected delimiter: %v", v)
		}
	case string:
		sw.writeJSONString(v)
	case json.Number:
		sw.writeRaw(v.String())
	case bool:
		if v {
			sw.writeRaw("true")
		} else {
			sw.writeRaw("false")
		}
	case nil:
		sw.writeRaw("null")
	default:
		sw.writeJSONEncoded(v)
	}
	return sw.err
}

// transformTokenObject reads a JSON object body (opening '{' already consumed).
func (t *Transformer) transformTokenObject(dec *json.Decoder, sw *streamWriter) error {
	sw.writeRaw("{")
	first := true

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		originalKey, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("expected string key, got %T", keyTok)
		}

		newKey, omit := t.applyRename(originalKey)
		fn := t.applyValueTransformer(originalKey)

		if omit && fn == nil {
			if err := skipValue(dec); err != nil {
				return err
			}
			continue
		}

		if !first {
			sw.writeRaw(",")
		}
		first = false

		sw.writeKey(newKey)

		if fn != nil {
			var goVal any
			if err := dec.Decode(&goVal); err != nil {
				return err
			}
			goVal = fn(goVal)
			sw.writeJSONEncoded(goVal)
		} else {
			if err := t.transformTokenValue(dec, sw); err != nil {
				return err
			}
		}

		if sw.err != nil {
			return sw.err
		}
	}

	if _, err := dec.Token(); err != nil { // consume '}'
		return err
	}
	sw.writeRaw("}")
	return sw.err
}

// transformTokenArray reads a JSON array body (opening '[' already consumed).
func (t *Transformer) transformTokenArray(dec *json.Decoder, sw *streamWriter) error {
	sw.writeRaw("[")
	first := true

	for dec.More() {
		if !first {
			sw.writeRaw(",")
		}
		first = false

		if err := t.transformTokenValue(dec, sw); err != nil {
			return err
		}
		if sw.err != nil {
			return sw.err
		}
	}

	if _, err := dec.Token(); err != nil { // consume ']'
		return err
	}
	sw.writeRaw("]")
	return sw.err
}

// skipValue consumes one complete JSON value from dec without writing anything.
func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	d, isDelim := tok.(json.Delim)
	if !isDelim || (d != '{' && d != '[') {
		return nil
	}
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d2, ok := tok.(json.Delim); ok {
			switch d2 {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

// ── streamWriter ─────────────────────────────────────────────────────────────

// streamWriterPool pools streamWriter structs including their buffer and fallback encoder.
var streamWriterPool = sync.Pool{
	New: func() any {
		buf := new(bytes.Buffer)
		sw := &streamWriter{buf: buf}
		sw.enc = json.NewEncoder(buf)
		sw.enc.SetEscapeHTML(false)
		return sw
	},
}

// streamWriter is a pooled, buffered writer for the streaming (token-based) path.
type streamWriter struct {
	w   io.Writer
	buf *bytes.Buffer
	enc *json.Encoder // fallback for unknown/complex types
	err error
}

func newStreamWriter(w io.Writer) *streamWriter {
	sw := streamWriterPool.Get().(*streamWriter)
	sw.buf.Reset()
	sw.w = w
	sw.err = nil
	return sw
}

func (sw *streamWriter) flush() error {
	err := sw.err
	if err == nil {
		_, err = sw.buf.WriteTo(sw.w)
	}
	sw.w = nil
	sw.err = nil
	streamWriterPool.Put(sw)
	return err
}

func (sw *streamWriter) writeRaw(s string) {
	if sw.err != nil {
		return
	}
	_, sw.err = io.WriteString(sw.buf, s)
}

// writeJSONString writes s as a JSON string directly to the buffer.
func (sw *streamWriter) writeJSONString(s string) {
	if sw.err != nil {
		return
	}
	b := sw.buf.AvailableBuffer()
	b = appendJSONString(b, s)
	_, sw.err = sw.buf.Write(b)
}

func (sw *streamWriter) writeKey(key string) {
	sw.writeJSONString(key)
	sw.writeRaw(":")
}

// writeJSONEncoded uses the pooled json.Encoder as a fallback for unknown types.
func (sw *streamWriter) writeJSONEncoded(v any) {
	if sw.err != nil {
		return
	}
	before := sw.buf.Len()
	sw.err = sw.enc.Encode(v)
	if sw.err == nil {
		if sw.buf.Len() > before && sw.buf.Bytes()[sw.buf.Len()-1] == '\n' {
			sw.buf.Truncate(sw.buf.Len() - 1)
		}
	}
}
