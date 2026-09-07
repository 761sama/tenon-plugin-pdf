// Package outline 实现 PDF 书签大纲（ISO 32000-1 §12.3.3）。
package outline

import (
	"gopkg.761sama.com/tenon-plugin-pdf/annot"
	"gopkg.761sama.com/tenon-plugin-pdf/color"
	"gopkg.761sama.com/tenon-plugin-pdf/object"
	"gopkg.761sama.com/tenon-plugin-pdf/writer"
)

// Item 一个大纲条目，可嵌套子条目。
type Item struct {
	Title    string
	Dest     annot.Destination
	Bold     bool
	Italic   bool
	Color    *color.RGB
	Children []*Item

	parent *Item
	ref    object.Ref
	closed bool
}

// SetClosed 设置该条目默认折叠子级。
func (it *Item) SetClosed(closed bool) *Item { it.closed = closed; return it }

// Add 添加子条目。
func (it *Item) Add(title string, dest annot.Destination) *Item {
	child := &Item{Title: title, Dest: dest, parent: it}
	it.Children = append(it.Children, child)
	return child
}

// Outline 书签大纲。
type Outline struct {
	roots []*Item
}

// Add 添加顶级条目。
func (o *Outline) Add(title string, dest annot.Destination) *Item {
	it := &Item{Title: title, Dest: dest}
	o.roots = append(o.roots, it)
	return it
}

// Empty 返回大纲是否为空。
func (o *Outline) Empty() bool { return len(o.roots) == 0 }

// Build 将大纲序列化为间接对象，返回 /Outlines 根引用。
// res 用于解析跳转目标。
func (o *Outline) Build(w *writer.Writer, res annot.Resolver) object.Ref {
	rootRef := w.Alloc()
	count := o.buildLevel(w, nil, rootRef, o.roots, res)
	root := object.NewDict()
	root.Set("Type", object.Name("Outlines"))
	if len(o.roots) > 0 {
		root.Set("First", o.roots[0].ref)
		root.Set("Last", o.roots[len(o.roots)-1].ref)
	}
	root.Set("Count", object.Int(count))
	w.Set(rootRef, root)
	return rootRef
}

// buildLevel 构建一层条目，返回可见条目总数。
func (o *Outline) buildLevel(w *writer.Writer, parent *Item, parentRef object.Ref, items []*Item, res annot.Resolver) int {
	for _, it := range items {
		it.ref = w.Alloc()
	}
	total := 0
	for i, it := range items {
		childCount := o.buildLevel(w, it, it.ref, it.Children, res)
		d := object.NewDict()
		d.Set("Title", object.TextStr(it.Title))
		d.Set("Parent", parentRef)
		if i > 0 {
			d.Set("Prev", items[i-1].ref)
		}
		if i < len(items)-1 {
			d.Set("Next", items[i+1].ref)
		}
		if len(it.Children) > 0 {
			d.Set("First", it.Children[0].ref)
			d.Set("Last", it.Children[len(it.Children)-1].ref)
			if it.closed {
				d.Set("Count", object.Int(-childCount))
			} else {
				d.Set("Count", object.Int(childCount))
			}
		}
		d.Set("Dest", res(it.Dest))
		var flags int
		if it.Italic {
			flags |= 1
		}
		if it.Bold {
			flags |= 2
		}
		if flags != 0 {
			d.Set("F", object.Int(flags))
		}
		if it.Color != nil {
			c := it.Color.Components()
			d.Set("C", object.Array{object.Real(c[0]), object.Real(c[1]), object.Real(c[2])})
		}
		w.Set(it.ref, d)
		total += 1 + childCount
	}
	return total
}
