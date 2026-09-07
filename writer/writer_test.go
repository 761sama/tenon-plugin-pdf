package writer

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"gopkg.761sama.com/tenon-plugin-pdf/object"
)

func TestWriteMinimalFile(t *testing.T) {
	w := New()
	catalog := w.Alloc()
	pages := w.Alloc()
	w.Set(catalog, object.NewDict().Set("Type", object.Name("Catalog")).Set("Pages", pages))
	w.Set(pages, object.NewDict().Set("Type", object.Name("Pages")).Set("Kids", object.Array{}).Set("Count", object.Int(0)))

	info := object.NewDict().Set("Producer", object.Str("test"))
	var buf bytes.Buffer
	n, err := w.WriteTo(&buf, catalog, info, []byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(buf.Len()) {
		t.Errorf("WriteTo returned %d, wrote %d bytes", n, buf.Len())
	}

	out := buf.String()
	for _, want := range []string{
		"%PDF-1.7", "1 0 obj", "2 0 obj", "xref", "trailer",
		"/Root 1 0 R", "/Info", "/ID [<30313233343536373839616263646566>",
		"startxref", "%%EOF",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}

	// 校验 xref 偏移量指向对象起始处
	xrefAt := strings.Index(out, "xref\n")
	var zero, total int
	var off int
	lines := strings.Split(out[xrefAt:], "\n")
	if _, err := fmt.Sscanf(lines[1], "%d %d", &zero, &total); err != nil || zero != 0 || total != 3 {
		t.Errorf("bad xref header %q", lines[1])
	}
	if _, err := fmt.Sscanf(lines[2], "%d", &off); err == nil && off != 0 {
		t.Errorf("free entry offset = %d", off)
	}
	var off1 int
	if _, err := fmt.Sscanf(lines[3], "%d", &off1); err != nil {
		t.Fatalf("bad xref entry %q", lines[3])
	}
	if !strings.HasPrefix(out[off1:], "1 0 obj") {
		t.Errorf("xref offset %d does not point to object 1", off1)
	}
}

func TestAllocSet(t *testing.T) {
	w := New()
	r1 := w.Alloc()
	r2 := w.Add(object.Int(1))
	if r1.Num != 1 || r2.Num != 2 || w.NumObjects() != 2 {
		t.Fatalf("refs: %v %v, n=%d", r1, r2, w.NumObjects())
	}
	w.Set(r1, object.Int(9))
	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf, r1, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "1 0 obj\n9\nendobj") {
		t.Errorf("Set did not fill object 1:\n%s", buf.String())
	}
}
