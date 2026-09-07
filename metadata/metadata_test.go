package metadata

import (
	"strings"
	"testing"
	"time"
)

func TestInfoDict(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	i := &Info{
		Title:        "标题 Title",
		Author:       "作者",
		Subject:      "测试",
		Keywords:     "go,pdf",
		Creator:      "unit-test",
		CreationDate: time.Date(2026, 9, 4, 12, 30, 0, 0, loc),
	}
	s := string(i.Dict().Encode(nil))
	for _, want := range []string{"/Title <FEFF", "/Keywords (go,pdf)", "/Creator (unit-test)", "/Producer (tenon-plugin-pdf)", "/CreationDate (D:20260904123000+08'00')"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
}

func TestEmpty(t *testing.T) {
	i := &Info{}
	if !i.Empty() {
		t.Error("zero Info should be empty")
	}
	// 空信息仍带默认 Producer
	if !strings.Contains(string(i.Dict().Encode(nil)), "/Producer") {
		t.Error("Producer should always present")
	}
}

func TestXMP(t *testing.T) {
	i := &Info{Title: "A&B", Author: "x<y>"}
	s := string(i.XMP())
	for _, want := range []string{"xpacket begin", "A&amp;B", "x&lt;y&gt;", "pdf:Producer", "xmpmeta"} {
		if !strings.Contains(s, want) {
			t.Errorf("XMP missing %q", want)
		}
	}
}
