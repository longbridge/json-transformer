package jsontransform

import (
	"encoding/json"
	"io"

	"github.com/valyala/fastjson"
)

var fjParserPool fastjson.ParserPool

// transformFastJSONBytes parses src with fastjson and walks the value tree,
// applying rename and value transformations without allocating per-token strings.
func (t *Transformer) transformFastJSONBytes(src []byte, dst io.Writer) error {
	p := fjParserPool.Get()
	v, err := p.ParseBytes(src)
	if err != nil {
		fjParserPool.Put(p)
		return err
	}
	sw := newStreamWriter(dst)
	t.walkFJValue(v, sw)
	// Return parser to pool before flushing: all values have been serialised into sw.buf.
	fjParserPool.Put(p)
	return sw.flush()
}

func (t *Transformer) walkFJValue(v *fastjson.Value, sw *streamWriter) {
	switch v.Type() {
	case fastjson.TypeObject:
		obj, _ := v.Object()
		t.walkFJObject(obj, sw)
	case fastjson.TypeArray:
		arr, _ := v.Array()
		t.walkFJArray(arr, sw)
	case fastjson.TypeString:
		b, _ := v.StringBytes()
		sw.writeJSONString(string(b))
	case fastjson.TypeNumber:
		sw.writeRaw(string(v.MarshalTo(nil)))
	case fastjson.TypeTrue:
		sw.writeRaw("true")
	case fastjson.TypeFalse:
		sw.writeRaw("false")
	case fastjson.TypeNull:
		sw.writeRaw("null")
	}
}

func (t *Transformer) walkFJObject(obj *fastjson.Object, sw *streamWriter) {
	sw.writeRaw("{")
	first := true
	obj.Visit(func(keyBytes []byte, v *fastjson.Value) {
		if sw.err != nil {
			return
		}
		originalKey := string(keyBytes)
		newKey, omit := t.applyRename(originalKey)
		fn := t.applyValueTransformer(originalKey)

		if omit && fn == nil {
			return
		}

		if !first {
			sw.writeRaw(",")
		}
		first = false

		sw.writeKey(newKey)

		if fn != nil {
			goVal := fjToGoValue(v)
			goVal = fn(goVal)
			sw.writeJSONEncoded(goVal)
		} else {
			t.walkFJValue(v, sw)
		}
	})
	sw.writeRaw("}")
}

func (t *Transformer) walkFJArray(arr []*fastjson.Value, sw *streamWriter) {
	sw.writeRaw("[")
	for i, v := range arr {
		if i > 0 {
			sw.writeRaw(",")
		}
		t.walkFJValue(v, sw)
	}
	sw.writeRaw("]")
}

// fjToGoValue converts a fastjson.Value to a Go value for use in ValueTransformFunc.
// Numbers are returned as json.Number to preserve precision and match the behaviour
// of the json.Decoder stream path.
func fjToGoValue(v *fastjson.Value) any {
	switch v.Type() {
	case fastjson.TypeObject:
		obj, _ := v.Object()
		m := make(map[string]any, obj.Len())
		obj.Visit(func(k []byte, val *fastjson.Value) {
			m[string(k)] = fjToGoValue(val)
		})
		return m
	case fastjson.TypeArray:
		arr, _ := v.Array()
		s := make([]any, len(arr))
		for i, el := range arr {
			s[i] = fjToGoValue(el)
		}
		return s
	case fastjson.TypeString:
		b, _ := v.StringBytes()
		return string(b)
	case fastjson.TypeNumber:
		return json.Number(v.MarshalTo(nil))
	case fastjson.TypeTrue:
		return true
	case fastjson.TypeFalse:
		return false
	default: // TypeNull
		return nil
	}
}
