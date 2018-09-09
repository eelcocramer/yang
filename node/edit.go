package node

import (
	"fmt"

	"github.com/freeconf/gconf/c2"
	"github.com/freeconf/gconf/meta"
)

type editStrategy int

const (
	editUpsert editStrategy = iota + 1
	editInsert
	editUpdate
)

type editor struct {
	basePath   *Path
	useDefault bool
}

func (self editor) edit(from Selection, to Selection, s editStrategy) (err error) {
	if err := self.enter(from, to, false, s, true, true); err != nil {
		return err
	}
	return nil
}

func (self editor) enter(from Selection, to Selection, new bool, strategy editStrategy, root bool, bubble bool) error {
	if err := to.beginEdit(NodeRequest{New: new, Source: to, EditRoot: root}, bubble); err != nil {
		return err
	}
	if meta.IsList(from.Meta()) && !from.InsideList {
		if err := self.list(from, to, from.Meta().(*meta.List), new, strategy); err != nil {
			return err
		}
	} else {
		ml := NewContainerMetaList(from)
		m := ml.Next()
		for m != nil {
			var err error
			if meta.IsLeaf(m) {
				err = self.leaf(from, to, m.(meta.HasType), new, strategy)
			} else {
				err = self.node(from, to, m.(meta.HasDataDefs), new, strategy)
			}
			if err != nil {
				return err
			}
			m = ml.Next()
		}
	}
	if err := to.endEdit(NodeRequest{New: new, Source: to, EditRoot: root}, bubble); err != nil {
		return err
	}
	return nil
}

func (self editor) leaf(from Selection, to Selection, m meta.HasType, new bool, strategy editStrategy) error {
	r := FieldRequest{
		Request: Request{
			Selection: from,
			Path:      &Path{parent: from.Path, meta: m},
			Base:      self.basePath,
		},
		Meta: m,
	}
	useDefault := (strategy != editUpdate && new) || self.useDefault
	var hnd ValueHandle
	if err := from.GetValueHnd(&r, &hnd, useDefault); err != nil {
		return err
	}

	if hnd.Val != nil {
		// If there is a different choice selected, need to clear it
		// first if in upsert mode
		if strategy == editUpsert {
			if err := self.clearOnDifferentChoiceCase(to, m); err != nil {
				return err
			}
		}

		r.Selection = to
		if err := to.SetValueHnd(&r, &hnd); err != nil {
			return err
		}
	}
	return nil
}

func (self editor) clearOnDifferentChoiceCase(existing Selection, want meta.Meta) error {
	wantCase, valid := want.Parent().(*meta.ChoiceCase)
	if !valid {
		return nil
	}
	choice := wantCase.Parent().(*meta.Choice)
	existingCase, err := existing.Node.Choose(existing, choice)
	if err != nil {
		return err
	}
	if existingCase == wantCase || existingCase == nil {
		return nil
	}
	return self.clearChoiceCase(existing, existingCase)
}

func (self editor) clearChoiceCase(sel Selection, c *meta.ChoiceCase) error {
	i := newChoiceCaseIterator(sel, c)
	m := i.Next()
	for m != nil {
		if meta.IsLeaf(m) {
			if err := sel.ClearField(m.(meta.HasType)); err != nil {
				return err
			}
		} else {
			sub := sel.Find(m.(meta.Identifiable).Ident())
			if !sub.IsNil() {
				if err := sub.Delete(); err != nil {
					return err
				}
			}
		}
		m = i.Next()
	}
	return nil
}

func (self editor) node(from Selection, to Selection, m meta.HasDataDefs, new bool, strategy editStrategy) error {
	var newChild bool
	fromRequest := ChildRequest{
		Request: Request{
			Selection: from,
			Path:      &Path{parent: from.Path, meta: m},
			Base:      self.basePath,
		},
		Meta: m,
	}
	fromChild := from.Select(&fromRequest)
	if fromChild.LastErr != nil || fromChild.IsNil() {
		return fromChild.LastErr
	}
	toRequest := ChildRequest{
		Request: Request{
			Selection: to,
			Path:      fromRequest.Path,
			Base:      self.basePath,
		},
		From: fromChild,
		Meta: m,
	}
	toRequest.New = false
	toRequest.Selection = to
	toRequest.From = fromChild

	toChild := to.Select(&toRequest)
	if toChild.LastErr != nil {
		return toChild.LastErr
	}
	toRequest.New = true
	switch strategy {
	case editInsert:
		if !toChild.IsNil() {
			msg := fmt.Sprintf("Duplicate item '%s' found in '%s' ", m.Ident(), fromRequest.Path)
			return c2.NewErrC(msg, 409)
		}
		if toChild = to.Select(&toRequest); toChild.LastErr != nil {
			return toChild.LastErr
		}
		newChild = true
	case editUpsert:
		if toChild.IsNil() {
			if toChild = to.Select(&toRequest); toChild.LastErr != nil {
				return toChild.LastErr
			}
			newChild = true
		}
	case editUpdate:
		if toChild.IsNil() {
			msg := fmt.Sprintf("cannot update '%s' not found in '%s' container destination node ",
				m.Ident(), fromRequest.Path)
			return c2.NewErrC(msg, 404)
		}
	default:
		return c2.NewErrC("Stratgey not implemented", 501)
	}

	if toChild.IsNil() {
		msg := fmt.Sprintf("'%s' could not create '%s' container node ", toRequest.Path, m.Ident())
		return c2.NewErr(msg)
	}
	if err := self.enter(fromChild, toChild, newChild, strategy, false, false); err != nil {
		return err
	}
	return nil
}

func (self editor) list(from Selection, to Selection, m *meta.List, new bool, strategy editStrategy) error {
	p := *from.Path
	fromRequest := ListRequest{
		Request: Request{
			Selection: from,
			Path:      &p,
			Base:      self.basePath,
		},
		First: true,
		Meta:  m,
	}
	fromChild, key := from.SelectListItem(&fromRequest)
	if fromChild.LastErr != nil {
		return fromChild.LastErr
	} else if fromChild.IsNil() {
		return nil
	}
	p.key = key
	toRequest := ListRequest{
		Request: Request{
			Selection: to,
			Path:      &p,
			Base:      self.basePath,
		},
		First: true,
		Meta:  m,
	}
	empty := Selection{}
	var toChild Selection
	for !fromChild.IsNil() {
		var newItem bool
		toChild = empty

		toRequest.First = true
		toRequest.SetRow(fromRequest.Row64)
		toRequest.Selection = to
		toRequest.From = fromChild
		toRequest.Key = key
		p.key = key
		if len(key) > 0 {
			toRequest.New = false
			if toChild, _ = to.SelectListItem(&toRequest); toChild.LastErr != nil {
				return toChild.LastErr
			}
		}
		toRequest.New = true
		switch strategy {
		case editUpdate:
			if toChild.IsNil() {
				msg := fmt.Sprintf("'%v' not found in '%s' list node ", key, to.Path.String())
				return c2.NewErrC(msg, 404)
			}
		case editUpsert:
			if toChild.IsNil() {
				toChild, _ = to.SelectListItem(&toRequest)
				newItem = true
			}
		case editInsert:
			if !toChild.IsNil() {
				msg := "Duplicate item found with same key in list " + to.Path.String()
				return c2.NewErrC(msg, 409)
			}
			toChild, _ = to.SelectListItem(&toRequest)
			newItem = true
		default:
			return c2.NewErrC("Stratgey not implmented", 501)
		}

		if toChild.LastErr != nil {
			return toChild.LastErr
		} else if toChild.IsNil() {
			return c2.NewErr("Could not create destination list node " + to.Path.String())
		}

		if err := self.enter(fromChild, toChild, newItem, editUpsert, false, false); err != nil {
			return err
		}

		fromRequest.First = false
		fromRequest.Selection = fromChild
		fromRequest.New = false
		fromRequest.From = to
		fromRequest.Path.key = key
		fromRequest.IncrementRow()
		if fromChild, key = from.SelectListItem(&fromRequest); fromChild.LastErr != nil {
			return fromChild.LastErr
		}
	}
	return nil
}
