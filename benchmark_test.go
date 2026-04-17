package jsontransform_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	jsontransform "github.com/longbridge/json-transformer"
)

var benchInput = map[string]any{
	"firstName":   "Alice",
	"lastName":    "Smith",
	"userAge":     30,
	"emailAddr":   "alice@example.com",
	"phoneNumber": "+1-555-0100",
	"isActive":    true,
	"score":       99.5,
}

var benchJSON = `{"firstName":"Alice","lastName":"Smith","userAge":30,"emailAddr":"alice@example.com","phoneNumber":"+1-555-0100","isActive":true,"score":99.5}`

var benchJSONBytes = []byte(benchJSON)

func BenchmarkTransformMap(b *testing.B) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_, err := tr.TransformBytes(benchInput)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTransformBytes(b *testing.B) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_, err := tr.TransformBytes(benchJSONBytes)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTransformString(b *testing.B) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_, err := tr.TransformBytes(benchJSON)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTransformReader(b *testing.B) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		r := strings.NewReader(benchJSON)
		_, err := tr.TransformBytes(r)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTransformNoOp(b *testing.B) {
	tr := jsontransform.New() // no options = pass-through
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_, err := tr.TransformBytes(benchJSONBytes)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTransformParallel(b *testing.B) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := tr.TransformBytes(benchInput)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkTransformStruct(b *testing.B) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	u := User{UserName: "Alice", UserAge: 30}
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_, err := tr.TransformBytes(u)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTransformNoOp_Writer(b *testing.B) {
	tr := jsontransform.New()
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		if err := tr.Transform(benchJSONBytes, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTransformStream_NDJSON(b *testing.B) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	input := strings.Repeat(benchJSON+"\n", 10)
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		r := strings.NewReader(input)
		if err := tr.TransformStream(r, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTransformMap_Writer(b *testing.B) {
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		var buf bytes.Buffer
		if err := tr.Transform(benchInput, &buf); err != nil {
			b.Fatal(err)
		}
	}
}

// ─── Large JSON (100 MB) ─────────────────────────────────────────────────────

type largeRecord struct {
	UserID       int     `json:"userId"`
	FirstName    string  `json:"firstName"`
	LastName     string  `json:"lastName"`
	EmailAddress string  `json:"emailAddress"`
	Department   string  `json:"department"`
	CreatedAt    string  `json:"createdAt"`
	Score        float64 `json:"score"`
	IsActive     bool    `json:"isActive"`
}

var (
	largeJSONOnce  sync.Once
	largeJSONBytes []byte
)

func getLargeJSON() []byte {
	largeJSONOnce.Do(func() {
		firstNames := []string{"Alice", "Bob", "Charlie", "Diana", "Eve", "Frank", "Grace", "Henry"}
		lastNames := []string{"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis"}
		departments := []string{"Engineering", "Marketing", "Sales", "HR", "Finance", "Legal", "Design", "Ops"}

		const target = 100 * 1024 * 1024 // 100 MB

		var buf bytes.Buffer
		buf.Grow(target + 1024)
		buf.WriteByte('[')

		for i := 0; buf.Len() < target; i++ {
			if i > 0 {
				buf.WriteByte(',')
			}
			r := largeRecord{
				UserID:       i + 1,
				FirstName:    firstNames[i%len(firstNames)],
				LastName:     lastNames[i%len(lastNames)],
				EmailAddress: fmt.Sprintf("user%d@example.com", i+1),
				Department:   departments[i%len(departments)],
				CreatedAt:    "2024-01-15T10:30:00Z",
				Score:        float64(i%100) + 0.5,
				IsActive:     i%3 != 0,
			}
			b, _ := json.Marshal(r)
			buf.Write(b)
		}
		buf.WriteByte(']')
		largeJSONBytes = buf.Bytes()
	})
	return largeJSONBytes
}

func BenchmarkTransformLargeJSON(b *testing.B) {
	data := getLargeJSON()
	tr := jsontransform.New(jsontransform.WithRenameFunc(jsontransform.SnakeCaseRename()))
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if err := tr.Transform(data, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
