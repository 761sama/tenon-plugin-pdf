package object

import "testing"

// 测试标量对象（null、布尔、整数、实数、名称）的序列化及名称特殊字符转义。
func TestScalarEncode(t *testing.T) {
	cases := []struct {
		o    Object
		want string
	}{
		{Null, "null"},
		{Bool(true), "true"},
		{Bool(false), "false"},
		{Int(42), "42"},
		{Int(-7), "-7"},
		{Real(3.14), "3.14"},
		{Real(-0.5), "-0.5"},
		{Real(2), "2"},
		{Name("Type"), "/Type"},
		{Name("A B"), "/A#20B"},
		{Name("a(b"), "/a#28b"},
	}
	for _, c := range cases {
		if got := string(Serialize(c.o)); got != c.want {
			t.Errorf("Encode(%v) = %q, want %q", c.o, got, c.want)
		}
	}
}

// 测试字符串与十六进制字符串对象的序列化，含括号、反斜杠转义及非 ASCII 字节的八进制表示。
func TestStringEncode(t *testing.T) {
	cases := []struct {
		o    Object
		want string
	}{
		{Str("hello"), "(hello)"},
		{Str("a(b)c"), `(a\(b\)c)`},
		{Str("back\\slash"), `(back\\slash)`},
		{String("a\nb"), `(a\nb)`},
		{String{0xe4, 0xb8, 0xad}, `(\344\270\255)`},
		{HexString{0xde, 0xad}, "<DEAD>"},
	}
	for _, c := range cases {
		if got := string(Serialize(c.o)); got != c.want {
			t.Errorf("Encode = %q, want %q", got, c.want)
		}
	}
}

// 测试数组对象序列化，nil 元素输出为 null。
func TestArrayEncode(t *testing.T) {
	a := Array{Int(1), Str("x"), nil}
	want := `[1 (x) null]`
	if got := string(Serialize(a)); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// 测试字典对象序列化及 Set 保序替换、Get、Delete 操作。
func TestDictEncode(t *testing.T) {
	d := NewDict()
	d.Set("Type", Name("Page"))
	d.Set("Count", Int(2))
	want := "<< /Type /Page /Count 2 >>"
	if got := string(Serialize(d)); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// Set 替换已存在的键且不改变顺序
	d.Set("Count", Int(3))
	if got := string(Serialize(d)); got != "<< /Type /Page /Count 3 >>" {
		t.Errorf("replace failed: %q", got)
	}
	if _, ok := d.Get("Count"); !ok {
		t.Error("Get missing Count")
	}
	d.Delete("Type")
	if d.Len() != 1 {
		t.Errorf("after Delete Len = %d", d.Len())
	}
}

// 测试流对象序列化，自动写入 /Length 并拼接 stream/endstream 数据。
func TestStreamEncode(t *testing.T) {
	s := NewStream([]byte("BT /F1 12 Tf ET"))
	s.Dict.Set("Filter", Name("FlateDecode"))
	want := "<< /Filter /FlateDecode /Length 15 >>\nstream\nBT /F1 12 Tf ET\nendstream"
	if got := string(Serialize(s)); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// 测试间接引用对象序列化为 "N G R" 形式。
func TestRefEncode(t *testing.T) {
	r := Ref{Num: 12, Gen: 0}
	if got := string(Serialize(r)); got != "12 0 R" {
		t.Errorf("got %q", got)
	}
}
